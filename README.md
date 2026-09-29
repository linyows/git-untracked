# git-untracked

Propagate untracked files from the main worktree to other git worktrees — copy `.env`, symlink dev certificates, clone `node_modules` with copy-on-write — automatically on `git worktree add`.

## Install

```sh
brew install linyows/git-untracked/git-untracked
```

or

```sh
go install github.com/linyows/git-untracked/cmd/git-untracked@latest
```

## Usage

Create `.gituntracked` in the main worktree and commit it:

```yaml
version: 1
defaults:
  conflict: skip        # skip | overwrite | backup
  link: relative        # relative | absolute
rules:
  - path: .env
    action: copy
  - path: certs/
    action: link
  - path: node_modules/
    action: clone       # clonefile(2) on macOS, FICLONE on Linux, falls back to copy
```

Then:

```sh
git untracked install-hook   # sync on every `git worktree add`
git worktree add ../feature -b feature

git untracked status         # missing / ok / differs / tracked / nosource
git untracked sync [--all] [--force] [--dry-run]
git untracked clean          # remove synced files that are still unmodified
```

Personal rules can be placed in `.git/info/gituntracked` with the same format; they are merged over the shared file.

macOS and Linux are supported.

## License

MIT
