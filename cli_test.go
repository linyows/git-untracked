package untracked

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linyows/git-untracked/config"
	"github.com/linyows/git-untracked/hook"
)

// fixture is a repository with a main worktree and one linked worktree.
type fixture struct {
	t    *testing.T
	main string
	wt   string
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func newFixture(t *testing.T, rules string) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, main: filepath.Join(tmp, "main"), wt: filepath.Join(tmp, "wt")}
	if err := os.MkdirAll(f.main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, f.main, "git", "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(f.main, ".gitignore"), ".env\n.env.*\ncerts/\nnode_modules/\n*.local.yml\n")
	writeFile(t, filepath.Join(f.main, "tracked.txt"), "tracked")
	writeFile(t, filepath.Join(f.main, config.FileName), "version: 1\nrules:\n"+rules)
	run(t, f.main, "git", "add", ".")
	run(t, f.main, "git", "commit", "-q", "-m", "init")

	writeFile(t, filepath.Join(f.main, ".env"), "SECRET=1\n")
	writeFile(t, filepath.Join(f.main, "certs", "dev.pem"), "cert")
	writeFile(t, filepath.Join(f.main, "node_modules", "foo", "index.js"), "module")
	writeFile(t, filepath.Join(f.main, "config", "a.local.yml"), "a")
	writeFile(t, filepath.Join(f.main, "config", "b.local.yml"), "b")
	run(t, f.main, "git", "worktree", "add", "-q", f.wt, "-b", "feat")
	return f
}

func (f *fixture) cli(dir string, args ...string) (string, int) {
	f.t.Helper()
	var out, errb bytes.Buffer
	code := RunCLI(Env{Out: &out, Err: &errb, Args: args, Dir: dir, Version: "test"})
	return out.String() + errb.String(), code
}

func (f *fixture) read(p string) string {
	f.t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

const defaultRules = `  - path: .env
    action: copy
  - path: certs/
    action: link
  - path: node_modules/
    action: clone
  - path: config/*.local.yml
    action: copy
  - path: missing.txt
    action: copy
`

func TestSync(t *testing.T) {
	f := newFixture(t, defaultRules)

	out, code := f.cli(f.wt, "status")
	if code != 0 || strings.Count(out, "  missing ") != 5 || !strings.Contains(out, "nosource") {
		t.Fatalf("status before sync (%d):\n%s", code, out)
	}

	out, code = f.cli(f.wt, "sync", "--dry-run")
	if code != 0 || !strings.Contains(out, "(dry-run)") {
		t.Fatalf("dry-run (%d):\n%s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(f.wt, ".env")); !os.IsNotExist(err) {
		t.Fatal("dry-run created a file")
	}

	out, code = f.cli(f.wt, "sync")
	if code != 0 || strings.Count(out, "created") != 5 {
		t.Fatalf("sync (%d):\n%s", code, out)
	}
	if got := f.read(filepath.Join(f.wt, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env = %q", got)
	}
	if l, _ := os.Readlink(filepath.Join(f.wt, "certs")); l != "../main/certs" {
		t.Errorf("certs link = %q", l)
	}
	if fi, err := os.Lstat(filepath.Join(f.wt, "node_modules")); err != nil || !fi.IsDir() {
		t.Errorf("node_modules should be a real directory: %v", err)
	}
	if got := f.read(filepath.Join(f.wt, "config", "b.local.yml")); got != "b" {
		t.Errorf("glob copy = %q", got)
	}

	out, code = f.cli(f.wt, "sync")
	if code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("second sync (%d):\n%s", code, out)
	}
}

func TestSyncConflict(t *testing.T) {
	f := newFixture(t, defaultRules)
	f.cli(f.wt, "sync")
	env := filepath.Join(f.wt, ".env")
	writeFile(t, env, "LOCAL\n")

	out, _ := f.cli(f.wt, "sync")
	if !strings.Contains(out, "skipped") || f.read(env) != "LOCAL\n" {
		t.Fatalf("skip:\n%s", out)
	}
	out, _ = f.cli(f.wt, "status")
	if !strings.Contains(out, "differs    copy   .env") {
		t.Errorf("status:\n%s", out)
	}

	// Private config overrides the default conflict policy.
	writeFile(t, filepath.Join(f.main, ".git", "info", config.PrivateFileName), "version: 1\ndefaults:\n  conflict: backup\n")
	out, _ = f.cli(f.wt, "sync")
	if !strings.Contains(out, "backed up") || f.read(env+".orig") != "LOCAL\n" || f.read(env) != "SECRET=1\n" {
		t.Fatalf("backup:\n%s", out)
	}

	writeFile(t, env, "LOCAL2\n")
	out, _ = f.cli(f.wt, "sync", "--force")
	if !strings.Contains(out, "overwrote") || f.read(env) != "SECRET=1\n" {
		t.Fatalf("force:\n%s", out)
	}
}

func TestSyncSkipsTracked(t *testing.T) {
	f := newFixture(t, "  - path: tracked.txt\n    action: link\n")
	out, code := f.cli(f.wt, "sync", "-v")
	if code != 0 || !strings.Contains(out, "tracked    link   tracked.txt") {
		t.Fatalf("(%d):\n%s", code, out)
	}
	if fi, _ := os.Lstat(filepath.Join(f.wt, "tracked.txt")); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("tracked file replaced by a symlink")
	}
}

func TestSyncDoesNotWriteThroughLinkedParent(t *testing.T) {
	f := newFixture(t, `  - path: node_modules/
    action: link
  - path: node_modules/foo/index.js
    action: copy
`)
	before := f.read(filepath.Join(f.main, "node_modules", "foo", "index.js"))
	f.cli(f.wt, "sync")
	out, _ := f.cli(f.wt, "sync", "--force")
	if !strings.Contains(out, "parent directory is a symlink") {
		t.Errorf("sync:\n%s", out)
	}
	if got := f.read(filepath.Join(f.main, "node_modules", "foo", "index.js")); got != before {
		t.Errorf("main worktree modified: %q", got)
	}
}

func TestSyncAllAndMain(t *testing.T) {
	f := newFixture(t, defaultRules)
	out, code := f.cli(f.main, "sync")
	if code != 0 || !strings.Contains(out, "main worktree") {
		t.Fatalf("main (%d):\n%s", code, out)
	}
	out, code = f.cli(f.main, "sync", "--all")
	if code != 0 || !strings.Contains(out, f.wt) || strings.Contains(out, f.main+"\n") {
		t.Fatalf("--all (%d):\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(f.wt, ".env")); err != nil {
		t.Error(err)
	}
	if _, code := f.cli(f.main, "sync", "--all", f.wt); code == 0 {
		t.Error("--all with args should fail")
	}
	if _, code := f.cli(f.main, "sync", os.TempDir()); code == 0 {
		t.Error("non-worktree argument should fail")
	}
}

func TestClean(t *testing.T) {
	f := newFixture(t, defaultRules)
	f.cli(f.wt, "sync")
	writeFile(t, filepath.Join(f.wt, "node_modules", "new.js"), "x")

	out, code := f.cli(f.wt, "clean")
	if code != 0 {
		t.Fatalf("(%d):\n%s", code, out)
	}
	for _, p := range []string{".env", "certs", "config/a.local.yml"} {
		if _, err := os.Lstat(filepath.Join(f.wt, p)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	if !strings.Contains(out, "kept       clone  node_modules (modified)") {
		t.Errorf("clean:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.main, "certs", "dev.pem")); err != nil {
		t.Error("clean must not touch the link target")
	}
}

func TestInit(t *testing.T) {
	f := newFixture(t, "")
	if out, code := f.cli(f.wt, "init"); code == 0 {
		t.Fatalf("init over existing file should fail:\n%s", out)
	}
	if out, code := f.cli(f.wt, "init", "--force"); code != 0 {
		t.Fatalf("(%d):\n%s", code, out)
	}
	if got := f.read(filepath.Join(f.main, config.FileName)); got != config.Template {
		t.Errorf("template not written to main worktree:\n%s", got)
	}
}

func TestNoConfig(t *testing.T) {
	f := newFixture(t, "")
	os.Remove(filepath.Join(f.main, config.FileName))
	out, code := f.cli(f.wt, "sync")
	if code == 0 || !strings.Contains(out, "git untracked init") {
		t.Errorf("(%d):\n%s", code, out)
	}
}

func TestHookOnWorktreeAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "git-untracked"), "./cmd/git-untracked")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := newFixture(t, defaultRules)
	out, code := f.cli(f.main, "install-hook")
	if code != 0 || !hook.Installed(filepath.Join(f.main, ".git", "hooks")) {
		t.Fatalf("install (%d):\n%s", code, out)
	}

	wt2 := filepath.Join(filepath.Dir(f.main), "wt2")
	run(t, f.main, "git", "worktree", "add", "-q", wt2, "-b", "feat2")
	if got := f.read(filepath.Join(wt2, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env in new worktree = %q", got)
	}

	// Switching branches in an existing worktree must not trigger sync, even
	// when the new branch points at the same commit.
	os.Remove(filepath.Join(wt2, ".env"))
	run(t, wt2, "git", "checkout", "-q", "-b", "feat3")
	if _, err := os.Lstat(filepath.Join(wt2, ".env")); !os.IsNotExist(err) {
		t.Error("sync ran on a normal checkout")
	}

	// A worktree created without checkout, as sparse-checkout setups do, is
	// synced on its first checkout.
	wt3 := filepath.Join(filepath.Dir(f.main), "wt3")
	run(t, f.main, "git", "worktree", "add", "-q", "--no-checkout", wt3, "-b", "feat4")
	if _, err := os.Lstat(filepath.Join(wt3, ".env")); !os.IsNotExist(err) {
		t.Error("sync ran before checkout")
	}
	run(t, wt3, "git", "sparse-checkout", "init", "--cone")
	run(t, wt3, "git", "sparse-checkout", "set", "--", "config")
	run(t, wt3, "git", "checkout", "-q", "feat4")
	if got := f.read(filepath.Join(wt3, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env in sparse worktree = %q", got)
	}
	os.Remove(filepath.Join(wt3, ".env"))
	run(t, wt3, "git", "checkout", "-q", "-b", "feat5")
	if _, err := os.Lstat(filepath.Join(wt3, ".env")); !os.IsNotExist(err) {
		t.Error("sync ran again on a synced sparse worktree")
	}

	out, code = f.cli(f.main, "uninstall-hook")
	if code != 0 || !strings.Contains(out, "deleted") {
		t.Errorf("uninstall (%d):\n%s", code, out)
	}
}

func TestSyncMarksWorktree(t *testing.T) {
	f := newFixture(t, defaultRules)
	w := Worktree{Path: f.wt}
	if synced(w) {
		t.Fatal("marked before sync")
	}
	f.cli(f.wt, "sync", "--dry-run")
	if synced(w) {
		t.Error("dry-run marked the worktree")
	}
	f.cli(f.wt, "sync")
	if !synced(w) {
		t.Error("sync did not mark the worktree")
	}
	if _, err := os.Stat(filepath.Join(f.main, ".git", SyncedMarker)); !os.IsNotExist(err) {
		t.Error("main worktree must not be marked")
	}
}

func TestPostCheckout(t *testing.T) {
	f := newFixture(t, defaultRules)
	zero := strings.Repeat("0", 40)
	head := strings.TrimSpace(run(t, f.wt, "git", "rev-parse", "HEAD"))

	if out, code := f.cli(f.wt, "post-checkout", zero, head); code == 0 {
		t.Errorf("wrong number of args should fail:\n%s", out)
	}
	if out, code := f.cli(f.main, "post-checkout", zero, head, "1"); code != 0 || strings.Contains(out, "created") {
		t.Errorf("main (%d):\n%s", code, out)
	}
	out, code := f.cli(f.wt, "post-checkout", zero, head, "1")
	if code != 0 || strings.Count(out, "created") != 5 {
		t.Fatalf("new worktree (%d):\n%s", code, out)
	}
	os.Remove(filepath.Join(f.wt, ".env"))
	if out, _ := f.cli(f.wt, "post-checkout", head, head, "1"); strings.Contains(out, "created") {
		t.Errorf("synced worktree was synced again:\n%s", out)
	}
}

func TestHooksDir(t *testing.T) {
	f := newFixture(t, "")
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	repo, err := OpenRepo(f.wt)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		hooksPath string
		want      string
	}{
		{"", filepath.Join(f.main, ".git", "hooks")},
		{".husky/_", filepath.Join(f.main, ".husky", "_")},
		{"~/hooks", filepath.Join(home, "hooks")},
		{"/abs/hooks", "/abs/hooks"},
	}
	for _, tt := range tests {
		// The unset case comes first, so there is nothing to unset.
		if tt.hooksPath != "" {
			run(t, f.main, "git", "config", "core.hooksPath", tt.hooksPath)
		}
		got, err := hooksDir(repo)
		if err != nil || got != tt.want {
			t.Errorf("core.hooksPath=%q: got %q (%v), want %q", tt.hooksPath, got, err, tt.want)
		}
	}
}

func TestInstallHookDetectsManagers(t *testing.T) {
	f := newFixture(t, "")
	writeFile(t, filepath.Join(f.main, "lefthook.yml"), "")
	out, code := f.cli(f.main, "install-hook")
	if code != 0 || !strings.Contains(out, "lefthook detected") {
		t.Fatalf("(%d):\n%s", code, out)
	}
	if hook.Installed(filepath.Join(f.main, ".git", "hooks")) {
		t.Error("hook installed despite lefthook")
	}
	if _, code := f.cli(f.main, "install-hook", "--force"); code != 0 {
		t.Error("--force failed")
	}
	if !hook.Installed(filepath.Join(f.main, ".git", "hooks")) {
		t.Error("--force did not install")
	}

	os.Remove(filepath.Join(f.main, "lefthook.yml"))
	run(t, f.main, "git", "config", "core.hooksPath", ".husky/_")
	out, _ = f.cli(f.main, "install-hook")
	if !strings.Contains(out, "husky detected") {
		t.Errorf("husky:\n%s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := RunCLI(Env{Out: &out, Err: &errb, Args: []string{"nope"}}); code != ExitErr {
		t.Errorf("code = %d", code)
	}
	if code := RunCLI(Env{Out: &out, Err: &errb, Args: []string{"sync", "-h"}}); code != ExitOK {
		t.Errorf("-h code = %d", code)
	}
}
