package untracked

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/linyows/git-untracked/config"
	"github.com/linyows/git-untracked/hook"
)

const (
	// ExitOK for exit code
	ExitOK int = 0

	// ExitErr for exit code
	ExitErr int = 1
)

// Env is the environment the CLI runs in.
type Env struct {
	Out, Err io.Writer
	Args     []string
	Dir      string
	Version  string
	Commit   string
	Date     string
}

const usage = `Usage: git untracked <command> [options]

Propagate untracked files from the main worktree to other worktrees.

Commands:
  init              Create %[1]s in the main worktree
  sync              Copy, link or clone files into worktrees
  status            Show the state of each rule
  clean             Remove files created by sync (unmodified ones only)
  install-hook      Run sync automatically on 'git worktree add'
  uninstall-hook    Remove the hook installed by install-hook
  version           Print the version

Run 'git untracked <command> -h' for command options.
`

// RunCLI runs the CLI and returns the exit code.
func RunCLI(env Env) int {
	if env.Dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(env.Err, err)
			return ExitErr
		}
		env.Dir = wd
	}
	c := &cli{env: env}
	if len(env.Args) == 0 {
		fmt.Fprintf(env.Err, usage, config.FileName)
		return ExitErr
	}
	cmd, args := env.Args[0], env.Args[1:]
	var err error
	switch cmd {
	case "init":
		err = c.init(args)
	case "sync":
		err = c.sync(args)
	case "status":
		err = c.status(args)
	case "clean":
		err = c.clean(args)
	case "install-hook":
		err = c.installHook(args)
	case "uninstall-hook":
		err = c.uninstallHook(args)
	case "version", "-v", "--version":
		fmt.Fprintf(env.Out, "git-untracked %s (%s, %s)\n", env.Version, env.Commit, env.Date)
	case "help", "-h", "--help":
		fmt.Fprintf(env.Out, usage, config.FileName)
	default:
		fmt.Fprintf(env.Err, "unknown command: %s\n\n", cmd)
		fmt.Fprintf(env.Err, usage, config.FileName)
		return ExitErr
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	if err != nil {
		fmt.Fprintf(env.Err, "git-untracked: %s\n", err)
		return ExitErr
	}
	return ExitOK
}

type cli struct {
	env Env
}

type flags struct {
	*flag.FlagSet
	dryRun  bool
	verbose bool
	force   bool
	all     bool
}

func (c *cli) flags(name, synopsis string, dryRun, verbose, force, all bool) *flags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(c.env.Err)
	f := &flags{FlagSet: fs}
	fs.Usage = func() {
		fmt.Fprintf(c.env.Err, "Usage: git untracked %s %s\n\nOptions:\n", name, synopsis)
		fs.PrintDefaults()
	}
	if dryRun {
		fs.BoolVar(&f.dryRun, "n", false, "")
		fs.BoolVar(&f.dryRun, "dry-run", false, "show what would be done without changing anything")
	}
	if verbose {
		fs.BoolVar(&f.verbose, "v", false, "")
		fs.BoolVar(&f.verbose, "verbose", false, "also show entries that need no change")
	}
	if force {
		fs.BoolVar(&f.force, "f", false, "")
		fs.BoolVar(&f.force, "force", false, forceUsage[name])
	}
	if all {
		fs.BoolVar(&f.all, "a", false, "")
		fs.BoolVar(&f.all, "all", false, "target all linked worktrees")
	}
	return f
}

var forceUsage = map[string]string{
	"init":         "overwrite an existing " + config.FileName,
	"sync":         "overwrite differing files regardless of the conflict setting",
	"install-hook": "install even if husky or lefthook is detected",
}

func (c *cli) init(args []string) error {
	f := c.flags("init", "[options]", true, false, true, false)
	if err := f.Parse(args); err != nil {
		return err
	}
	repo, err := OpenRepo(c.env.Dir)
	if err != nil {
		return err
	}
	p := filepath.Join(repo.Main().Path, config.FileName)
	if _, err := os.Stat(p); err == nil && !f.force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", p)
	}
	if !f.dryRun {
		if err := os.WriteFile(p, []byte(config.Template), 0o644); err != nil { //nolint:gosec // committed config file
			return err
		}
	}
	fmt.Fprintf(c.env.Out, "created %s\n", p)
	return nil
}

// targets returns the worktrees selected by args or --all, defaulting to the
// worktree containing the current directory.
func (c *cli) targets(repo *Repo, f *flags) ([]Worktree, error) {
	if f.all {
		if f.NArg() > 0 {
			return nil, errors.New("--all cannot be used with worktree arguments")
		}
		var out []Worktree
		for _, w := range repo.Worktrees {
			if !w.Main && !w.Prunable {
				out = append(out, w)
			}
		}
		return out, nil
	}
	paths := f.Args()
	if len(paths) == 0 {
		paths = []string{c.env.Dir}
	}
	var out []Worktree
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.env.Dir, p)
		}
		w, err := repo.Find(p)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

func (c *cli) prepare(f *flags, args []string) (*Repo, []config.Rule, []Worktree, error) {
	if err := f.Parse(args); err != nil {
		return nil, nil, nil, err
	}
	repo, err := OpenRepo(c.env.Dir)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, err := LoadConfig(repo)
	if err != nil {
		return nil, nil, nil, err
	}
	wts, err := c.targets(repo, f)
	if err != nil {
		return nil, nil, nil, err
	}
	return repo, cfg.Resolved(), wts, nil
}

func (c *cli) sync(args []string) error {
	f := c.flags("sync", "[options] [<worktree>...]", true, true, true, true)
	repo, rules, wts, err := c.prepare(f, args)
	if err != nil {
		return err
	}
	return c.each(repo, wts, rules, false, func(it Item) (Outcome, string, error) {
		return Apply(it, Options{Force: f.force, DryRun: f.dryRun})
	}, f)
}

func (c *cli) clean(args []string) error {
	f := c.flags("clean", "[options] [<worktree>...]", true, true, false, true)
	repo, rules, wts, err := c.prepare(f, args)
	if err != nil {
		return err
	}
	return c.each(repo, wts, rules, true, func(it Item) (Outcome, string, error) {
		return Clean(it, Options{DryRun: f.dryRun})
	}, f)
}

func (c *cli) status(args []string) error {
	f := c.flags("status", "[options] [<worktree>...]", false, false, false, true)
	repo, rules, wts, err := c.prepare(f, args)
	if err != nil {
		return err
	}
	for i, w := range wts {
		if i > 0 {
			fmt.Fprintln(c.env.Out)
		}
		fmt.Fprintln(c.env.Out, w.Path)
		if w.Main {
			fmt.Fprintln(c.env.Out, "  (main worktree: source of all rules)")
			continue
		}
		items, err := Plan(repo.Main().Path, w.Path, rules, false)
		if err != nil {
			return err
		}
		for _, it := range items {
			c.line(string(it.State), it, it.Note)
		}
	}
	return nil
}

func (c *cli) each(repo *Repo, wts []Worktree, rules []config.Rule, deep bool,
	fn func(Item) (Outcome, string, error), f *flags) error {
	failed := 0
	for i, w := range wts {
		if i > 0 {
			fmt.Fprintln(c.env.Out)
		}
		prefix := ""
		if f.dryRun {
			prefix = "(dry-run) "
		}
		fmt.Fprintf(c.env.Out, "%s%s\n", prefix, w.Path)
		if w.Main {
			fmt.Fprintln(c.env.Out, "  (main worktree: nothing to do)")
			continue
		}
		items, err := Plan(repo.Main().Path, w.Path, rules, deep)
		if err != nil {
			return err
		}
		shown := 0
		for _, it := range items {
			out, note, err := fn(it)
			if err != nil {
				failed++
				c.line(string(OutcomeError), it, err.Error())
				shown++
				continue
			}
			if out == OutcomeNone {
				if !f.verbose {
					continue
				}
				c.line(string(it.State), it, it.Note)
			} else {
				c.line(string(out), it, note)
			}
			shown++
		}
		if shown == 0 {
			fmt.Fprintln(c.env.Out, "  up to date")
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d error(s)", failed)
	}
	return nil
}

func (c *cli) line(state string, it Item, note string) {
	s := fmt.Sprintf("  %-10s %-6s %s", state, it.Rule.Action, it.Rel)
	if note != "" {
		s += " (" + note + ")"
	}
	fmt.Fprintln(c.env.Out, s)
}

// hooksDir returns the directory git runs hooks from.
func hooksDir(repo *Repo) string {
	main := repo.Main().Path
	if p := GitConfig(main, "core.hooksPath"); p != "" {
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(main, p)
		}
		return p
	}
	return filepath.Join(repo.CommonDir, "hooks")
}

// hookManager detects a hook manager that would own post-checkout.
func hookManager(repo *Repo) string {
	main := repo.Main().Path
	if strings.Contains(GitConfig(main, "core.hooksPath"), ".husky") {
		return "husky"
	}
	for _, n := range []string{"lefthook.yml", "lefthook.yaml", ".lefthook.yml", ".lefthook.yaml"} {
		if _, err := os.Stat(filepath.Join(main, n)); err == nil {
			return "lefthook"
		}
	}
	return ""
}

const huskyGuide = `husky detected. Add the following to .husky/post-checkout:

%s

Or run 'git untracked install-hook --force' to install into %s anyway.
`

const lefthookGuide = `lefthook detected. Add the following to your lefthook config:

post-checkout:
  commands:
    git-untracked:
      run: case "{1}" in *[!0]*) ;; *) if [ "{3}" = "1" ]; then git untracked sync || :; fi ;; esac

Or run 'git untracked install-hook --force' to install into %s anyway
(lefthook may overwrite it on 'lefthook install').
`

func (c *cli) installHook(args []string) error {
	f := c.flags("install-hook", "[options]", true, false, true, false)
	if err := f.Parse(args); err != nil {
		return err
	}
	repo, err := OpenRepo(c.env.Dir)
	if err != nil {
		return err
	}
	dir := hooksDir(repo)
	if !f.force {
		switch hookManager(repo) {
		case "husky":
			fmt.Fprintf(c.env.Out, huskyGuide, hook.Block, dir)
			return nil
		case "lefthook":
			fmt.Fprintf(c.env.Out, lefthookGuide, dir)
			return nil
		}
	}
	res, err := hook.Install(dir, f.dryRun)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.env.Out, "%s %s\n", res, hook.Path(dir))
	return nil
}

func (c *cli) uninstallHook(args []string) error {
	f := c.flags("uninstall-hook", "[options]", true, false, false, false)
	if err := f.Parse(args); err != nil {
		return err
	}
	repo, err := OpenRepo(c.env.Dir)
	if err != nil {
		return err
	}
	dir := hooksDir(repo)
	res, err := hook.Uninstall(dir, f.dryRun)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.env.Out, "%s %s\n", res, hook.Path(dir))
	return nil
}
