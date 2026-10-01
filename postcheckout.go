package untracked

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SyncedMarker is created in a linked worktree's private git dir after it has
// been synced, so that the post-checkout hook syncs each worktree only once.
const SyncedMarker = "git-untracked-synced"

// shouldSync decides from the post-checkout arguments whether the checkout
// populated a new worktree.
//
// `git worktree add` passes an all-zero previous HEAD. A worktree created with
// --no-checkout, as sparse-checkout setups do, gets its files from a later
// `git checkout` in which the previous and new HEAD are the same commit; the
// marker tells that apart from an ordinary checkout of the current commit.
func shouldSync(prev, next, flag string, main, synced bool) (bool, string) {
	switch {
	case flag != "1":
		return false, "file checkout"
	case main:
		return false, "main worktree"
	case isNullOID(prev):
		return true, "new worktree"
	case prev == next && !synced:
		return true, "first checkout of a worktree created without checkout"
	case prev == next:
		return false, "already synced"
	default:
		return false, "branch switch"
	}
}

func isNullOID(s string) bool {
	return s != "" && strings.Trim(s, "0") == ""
}

func markerPath(w Worktree) (string, error) {
	dir, err := gitDir(w.Path)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SyncedMarker), nil
}

func synced(w Worktree) bool {
	p, err := markerPath(w)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func markSynced(w Worktree) error {
	p, err := markerPath(w)
	if err != nil {
		return err
	}
	return os.WriteFile(p, nil, 0o644) //nolint:gosec // empty marker in .git
}

func (c *cli) postCheckout(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: git untracked post-checkout <previous HEAD> <new HEAD> <flag>")
	}
	repo, err := OpenRepo(c.env.Dir)
	if err != nil {
		return err
	}
	w, err := repo.Find(c.env.Dir)
	if err != nil {
		return err
	}
	ok, reason := shouldSync(args[0], args[1], args[2], w.Main, !w.Main && synced(w))
	if !ok {
		c.debug.printf("post-checkout: skip (%s)", reason)
		return nil
	}
	c.debug.printf("post-checkout: sync (%s)", reason)
	return c.sync(nil)
}
