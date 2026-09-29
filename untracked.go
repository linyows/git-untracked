// Package untracked propagates untracked files from the main worktree to
// linked worktrees by copying, symlinking or cloning them.
package untracked

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/linyows/git-untracked/config"
)

// State is the state of an item in a worktree.
type State string

// States
const (
	StateOK       State = "ok"
	StateMissing  State = "missing"
	StateDiffers  State = "differs"
	StateTracked  State = "tracked"
	StateNoSource State = "nosource"
)

// Item is a single path matched by a rule.
type Item struct {
	Rule  config.Rule
	Rel   string // slash-separated path relative to the worktree root
	Src   string // absolute path in the main worktree
	Dst   string // absolute path in the target worktree
	State State
	Note  string

	// blocked is set when writing Dst would escape the worktree.
	blocked bool
}

// LoadConfig reads the shared and private configs and merges them.
func LoadConfig(repo *Repo) (*config.Config, error) {
	shared, err := config.Load(filepath.Join(repo.Main().Path, config.FileName))
	if err != nil {
		return nil, err
	}
	private, err := config.Load(filepath.Join(repo.CommonDir, "info", config.PrivateFileName))
	if err != nil {
		return nil, err
	}
	if shared == nil && private == nil {
		return nil, fmt.Errorf("%s not found in %s (run `git untracked init`)", config.FileName, repo.Main().Path)
	}
	return config.Merge(shared, private), nil
}

// Plan expands rules against the main worktree and inspects each match in
// the target worktree. Later rules override earlier ones for the same path.
func Plan(mainRoot, root string, rules []config.Rule, deep bool) ([]Item, error) {
	var items []Item
	index := map[string]int{}
	for _, r := range rules {
		rels, err := expand(mainRoot, r)
		if err != nil {
			return nil, err
		}
		if len(rels) == 0 {
			items = append(items, Item{Rule: r, Rel: r.Pattern(), State: StateNoSource})
			continue
		}
		for _, rel := range rels {
			it := Item{
				Rule: r,
				Rel:  rel,
				Src:  filepath.Join(mainRoot, filepath.FromSlash(rel)),
				Dst:  filepath.Join(root, filepath.FromSlash(rel)),
			}
			if i, ok := index[rel]; ok {
				items[i] = it
				continue
			}
			index[rel] = len(items)
			items = append(items, it)
		}
	}

	var rels []string
	for _, it := range items {
		if it.State == "" {
			rels = append(rels, it.Rel)
		}
	}
	tracked, err := trackedUnder(root, rels)
	if err != nil {
		return nil, err
	}
	for i := range items {
		it := &items[i]
		if it.State != "" {
			continue
		}
		if tracked[it.Rel] {
			it.State = StateTracked
			continue
		}
		if err := inspect(root, it, deep); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// expand returns slash-separated paths in mainRoot matched by the rule.
func expand(mainRoot string, r config.Rule) ([]string, error) {
	pattern := path.Clean(r.Pattern())
	if !doublestar.ValidatePattern(pattern) {
		return nil, fmt.Errorf("invalid pattern: %s", r.Path)
	}
	var matches []string
	if hasMeta(pattern) {
		m, err := doublestar.Glob(os.DirFS(mainRoot), pattern)
		if err != nil {
			return nil, err
		}
		matches = m
	} else if _, err := os.Lstat(filepath.Join(mainRoot, filepath.FromSlash(pattern))); err == nil {
		matches = []string{pattern}
	}
	var out []string
	for _, m := range matches {
		if m == ".git" || strings.HasPrefix(m, ".git/") {
			continue
		}
		if r.DirOnly() {
			fi, err := os.Stat(filepath.Join(mainRoot, filepath.FromSlash(m)))
			if err != nil || !fi.IsDir() {
				continue
			}
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func hasMeta(p string) bool {
	return strings.ContainsAny(p, `*?[{\`)
}

// inspect sets the state of it in the worktree at root.
func inspect(root string, it *Item, deep bool) error {
	if ok, err := insideRoot(root, filepath.Dir(it.Dst)); err != nil {
		return err
	} else if !ok {
		it.State = StateDiffers
		it.Note = "parent directory is a symlink out of the worktree"
		it.blocked = true
		return nil
	}
	di, err := os.Lstat(it.Dst)
	if errors.Is(err, fs.ErrNotExist) {
		it.State = StateMissing
		return nil
	}
	if err != nil {
		return err
	}
	if it.Rule.Action == config.ActionLink {
		it.State = stateOf(linkPointsTo(it.Dst, it.Src))
		return nil
	}
	if di.Mode()&fs.ModeSymlink != 0 {
		it.State = StateDiffers
		it.Note = "is a symlink"
		return nil
	}
	si, err := os.Stat(it.Src)
	if err != nil {
		return err
	}
	if si.IsDir() {
		if !di.IsDir() {
			it.State = StateDiffers
			return nil
		}
		if !deep {
			it.State = StateOK
			return nil
		}
		same, err := sameTree(realSrc(it.Src), it.Dst)
		if err != nil {
			return err
		}
		it.State = stateOf(same)
		return nil
	}
	same, err := sameFile(it.Src, it.Dst)
	if err != nil {
		return err
	}
	it.State = stateOf(same)
	return nil
}

func stateOf(ok bool) State {
	if ok {
		return StateOK
	}
	return StateDiffers
}

// insideRoot reports whether dir, after resolving symlinks of its existing
// ancestors, stays inside root. It prevents writing into the main worktree
// through a directory that an earlier rule turned into a symlink.
func insideRoot(root, dir string) (bool, error) {
	p := dir
	for {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			rel, err := filepath.Rel(root, real)
			if err != nil {
				return false, err
			}
			return rel == "." || !strings.HasPrefix(rel, ".."), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false, nil
		}
		p = parent
	}
}

// realSrc resolves a top-level symlink in the main worktree so that copy and
// clone duplicate the content rather than the link.
func realSrc(src string) string {
	if r, err := filepath.EvalSymlinks(src); err == nil {
		return r
	}
	return src
}

// Outcome is the result of applying or cleaning an item.
type Outcome string

// Outcomes
const (
	OutcomeNone      Outcome = ""
	OutcomeCreated   Outcome = "created"
	OutcomeOverwrote Outcome = "overwrote"
	OutcomeBackedUp  Outcome = "backed up"
	OutcomeSkipped   Outcome = "skipped"
	OutcomeRemoved   Outcome = "removed"
	OutcomeKept      Outcome = "kept"
	OutcomeError     Outcome = "error"
)

// Options controls Apply and Clean.
type Options struct {
	Force  bool
	DryRun bool
}

// Apply brings the item into the expected state. The returned note carries
// details such as the backup path or a clone fallback.
func Apply(it Item, opt Options) (Outcome, string, error) {
	switch it.State {
	case StateMissing:
		note, err := create(it, opt.DryRun)
		if err != nil {
			return OutcomeError, "", err
		}
		return OutcomeCreated, note, nil
	case StateDiffers:
		if it.blocked {
			return OutcomeSkipped, it.Note, nil
		}
		conflict := it.Rule.Conflict
		if opt.Force {
			conflict = config.ConflictOverwrite
		}
		switch conflict {
		case config.ConflictOverwrite:
			if !opt.DryRun {
				if err := os.RemoveAll(it.Dst); err != nil {
					return OutcomeError, "", err
				}
			}
			note, err := create(it, opt.DryRun)
			if err != nil {
				return OutcomeError, "", err
			}
			return OutcomeOverwrote, note, nil
		case config.ConflictBackup:
			bak := backupPath(it.Dst)
			if !opt.DryRun {
				if err := os.Rename(it.Dst, bak); err != nil {
					return OutcomeError, "", err
				}
			}
			note, err := create(it, opt.DryRun)
			if err != nil {
				return OutcomeError, "", err
			}
			return OutcomeBackedUp, joinNote("to "+filepath.Base(bak), note), nil
		default:
			return OutcomeSkipped, joinNote("differs", it.Note), nil
		}
	}
	return OutcomeNone, "", nil
}

func create(it Item, dryRun bool) (string, error) {
	if dryRun {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(it.Dst), 0o755); err != nil {
		return "", err
	}
	switch it.Rule.Action {
	case config.ActionLink:
		target, err := symlinkTarget(it.Src, it.Dst, it.Rule.Link != config.LinkAbsolute)
		if err != nil {
			return "", err
		}
		return "", os.Symlink(target, it.Dst)
	case config.ActionClone:
		fallback, err := clonePath(realSrc(it.Src), it.Dst)
		if err != nil {
			return "", err
		}
		if fallback {
			return "copy-on-write unsupported, copied", nil
		}
		return "", nil
	default:
		return "", copyPath(realSrc(it.Src), it.Dst)
	}
}

// Clean removes what sync created, but only when it still matches the main
// worktree. The item must be planned with deep=true.
func Clean(it Item, opt Options) (Outcome, string, error) {
	switch it.State {
	case StateOK:
		if !opt.DryRun {
			if err := os.RemoveAll(it.Dst); err != nil {
				return OutcomeError, "", err
			}
		}
		return OutcomeRemoved, "", nil
	case StateDiffers:
		return OutcomeKept, joinNote("modified", it.Note), nil
	}
	return OutcomeNone, "", nil
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + ", " + b
}
