// Package hook installs and removes the git-untracked block in a post-checkout hook.
package hook

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Name is the git hook that runs on `git worktree add`.
const Name = "post-checkout"

const (
	beginMarker = "# >>> git-untracked >>>"
	endMarker   = "# <<< git-untracked <<<"
	shebang     = "#!/bin/sh"
)

// Block is the snippet inserted into the hook. Whether to sync is decided by
// the post-checkout command, so that the hook itself stays trivial. Failures
// are swallowed so that `git worktree add` itself never fails because of us.
const Block = beginMarker + `
git untracked post-checkout "$@" || :
` + endMarker

// Result describes what Install or Uninstall did.
type Result string

// Results
const (
	Created   Result = "created"
	Appended  Result = "appended"
	Updated   Result = "updated"
	Unchanged Result = "unchanged"
	Removed   Result = "removed"
	Deleted   Result = "deleted"
	NotFound  Result = "not found"
)

// Path returns the hook file path in dir.
func Path(dir string) string {
	return filepath.Join(dir, Name)
}

// Install writes Block into the hook in dir. It is idempotent: an existing
// block is replaced, otherwise the block is appended to existing content.
func Install(dir string, dryRun bool) (Result, error) {
	p := Path(dir)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		if dryRun {
			return Created, nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return Created, os.WriteFile(p, []byte(shebang+"\n\n"+Block+"\n"), 0o755) //nolint:gosec // hooks must be executable
	}
	if err != nil {
		return "", err
	}

	content := string(b)
	var next string
	var res Result
	if before, after, ok := cut(content); ok {
		next = before + Block + after
		res = Updated
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		next = content + "\n" + Block + "\n"
		res = Appended
	}
	if next == string(b) {
		return Unchanged, nil
	}
	if dryRun {
		return res, nil
	}
	if err := writeKeepMode(p, next); err != nil {
		return "", err
	}
	return res, ensureExecutable(p)
}

// Uninstall removes Block from the hook in dir. When nothing but a shebang
// and blank lines remain, the file is deleted.
func Uninstall(dir string, dryRun bool) (Result, error) {
	p := Path(dir)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return NotFound, nil
	}
	if err != nil {
		return "", err
	}
	before, after, ok := cut(string(b))
	if !ok {
		return NotFound, nil
	}
	next := strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
	if isEmptyScript(next) {
		if dryRun {
			return Deleted, nil
		}
		return Deleted, os.Remove(p)
	}
	if dryRun {
		return Removed, nil
	}
	return Removed, writeKeepMode(p, next)
}

// Installed reports whether the hook in dir contains Block.
func Installed(dir string) bool {
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		return false
	}
	_, _, ok := cut(string(b))
	return ok
}

// cut splits content around the marker block, markers included.
func cut(content string) (before, after string, ok bool) {
	i := strings.Index(content, beginMarker)
	if i < 0 {
		return "", "", false
	}
	j := strings.Index(content[i:], endMarker)
	if j < 0 {
		return "", "", false
	}
	return content[:i], content[i+j+len(endMarker):], true
}

func isEmptyScript(s string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#!") {
			return false
		}
	}
	return true
}

func writeKeepMode(p, content string) error {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(p, []byte(content), mode)
}

func ensureExecutable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o111 == 0o111 {
		return nil
	}
	return os.Chmod(p, fi.Mode().Perm()|0o111)
}
