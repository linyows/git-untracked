package untracked

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is an entry of `git worktree list`.
type Worktree struct {
	Path     string
	Main     bool
	Prunable bool
}

// Repo is a git repository with its worktrees.
type Repo struct {
	CommonDir string
	Worktrees []Worktree
}

// Main returns the main worktree.
func (r *Repo) Main() Worktree {
	return r.Worktrees[0]
}

// Find returns the registered worktree containing path.
func (r *Repo) Find(path string) (Worktree, error) {
	real, err := realpath(path)
	if err != nil {
		return Worktree{}, err
	}
	var found Worktree
	for _, w := range r.Worktrees {
		if (real == w.Path || strings.HasPrefix(real, w.Path+string(filepath.Separator))) &&
			len(w.Path) > len(found.Path) {
			found = w
		}
	}
	if found.Path == "" {
		return Worktree{}, fmt.Errorf("not a worktree of this repository: %s", path)
	}
	return found, nil
}

// OpenRepo inspects the repository that dir belongs to.
func OpenRepo(dir string) (*Repo, error) {
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	wts, err := parseWorktreeList(out)
	if err != nil {
		return nil, err
	}
	common, err = realpath(common)
	if err != nil {
		return nil, err
	}
	return &Repo{CommonDir: common, Worktrees: wts}, nil
}

func parseWorktreeList(out string) ([]Worktree, error) {
	var wts []Worktree
	for i, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var w Worktree
		for _, line := range strings.Split(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.Path = val
			case "bare":
				return nil, errors.New("bare repositories are not supported")
			case "prunable":
				w.Prunable = true
			}
		}
		if w.Path == "" {
			continue
		}
		if p, err := realpath(w.Path); err == nil {
			w.Path = p
		}
		w.Main = i == 0
		wts = append(wts, w)
	}
	if len(wts) == 0 {
		return nil, errors.New("no worktree found")
	}
	return wts, nil
}

// Toplevel returns the root of the worktree containing dir.
func Toplevel(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return realpath(out)
}

// gitDir returns the private git dir of the worktree at dir.
func gitDir(dir string) (string, error) {
	return git(dir, "rev-parse", "--absolute-git-dir")
}

// GitConfig returns the value of key, or "" when it is unset.
func GitConfig(dir, key string) string {
	out, err := git(dir, "config", "--get", key)
	if err != nil {
		return ""
	}
	return out
}

// trackedUnder reports, for each of rels, whether it or anything below it is
// tracked in the worktree at root.
func trackedUnder(root string, rels []string) (map[string]bool, error) {
	res := make(map[string]bool, len(rels))
	if len(rels) == 0 {
		return res, nil
	}
	args := []string{"ls-files", "-z", "--"}
	for _, r := range rels {
		args = append(args, ":(literal)"+r)
	}
	out, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	for f := range strings.SplitSeq(out, "\x00") {
		if f == "" {
			continue
		}
		for _, r := range rels {
			if f == r || strings.HasPrefix(f, r+"/") {
				res[r] = true
			}
		}
	}
	return res, nil
}

// git runs git in dir. GIT_DIR and friends are dropped from the environment
// because git exports them to hooks, and they would otherwise override -C.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // arguments are passed without a shell
	cmd.Env = cleanEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func cleanEnv(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		switch k {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX":
			continue
		}
		out = append(out, e)
	}
	return out
}

func realpath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
