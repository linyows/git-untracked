<p align="right">English | <a href="https://github.com/linyows/git-untracked/blob/main/README.ja.md">日本語</a></p>

<p align="center"><br><br><br><br>
<h1>git-untracked</h1>
</p>

<p align="center">
  <strong>git-untracked</strong> brings your untracked files to every <a href="https://git-scm.com/docs/git-worktree">git worktree</a>.
</p><br><br><br><br>

<p align="center">
  <a href="https://github.com/linyows/git-untracked/actions/workflows/build.yml">
    <img alt="GitHub Workflow Status" src="https://img.shields.io/github/actions/workflow/status/linyows/git-untracked/build.yml?branch=main&style=for-the-badge&labelColor=666666">
  </a>
  <a href="https://github.com/linyows/git-untracked/releases">
    <img src="http://img.shields.io/github/release/linyows/git-untracked.svg?style=for-the-badge&labelColor=666666&color=DDDDDD" alt="GitHub Release">
  </a>
  <a href="http://godoc.org/github.com/linyows/git-untracked">
    <img src="http://img.shields.io/badge/go-docs-blue.svg?style=for-the-badge&labelColor=666666&color=DDDDDD" alt="Go Documentation">
  </a>
  <a href="https://github.com/linyows/git-untracked/blob/main/LICENSE">
    <img src="http://img.shields.io/badge/license-MIT-blue.svg?style=for-the-badge&labelColor=666666&color=DDDDDD" alt="MIT License">
  </a>
</p>

A new worktree created by `git worktree add` contains only tracked files, so `.env`, development certificates and `node_modules` have to be prepared again every time.
git-untracked copies, symlinks or clones them from the main worktree according to a `.gituntracked` file, and can do it automatically on `git worktree add`.

Features
--

- Three actions per path: `copy`, `link` (symlink) and `clone` (copy-on-write)
- Copy-on-write clones with `clonefile(2)` on macOS (APFS) and `FICLONE` on Linux (Btrfs, XFS), falling back to a plain copy elsewhere
- Automatic sync on `git worktree add` through a `post-checkout` hook
- Glob patterns including `**`
- Shared rules committed to the repository, plus personal rules kept in `.git/info`
- Never overwrites tracked files, and never writes into the main worktree through a symlinked directory
- `clean` removes only what is still identical to the main worktree

Install
--

```sh
brew install linyows/git-untracked/git-untracked
```

or

```sh
go install github.com/linyows/git-untracked/cmd/git-untracked@latest
```

macOS and Linux are supported.

Quick Start
--

```sh
cd path/to/repo              # the main worktree
git untracked init           # creates .gituntracked
$EDITOR .gituntracked
git untracked install-hook   # sync on every `git worktree add`

git worktree add ../feature -b feature
# .env, certs/ and node_modules/ are ready in ../feature
```

Configuration
--

`.gituntracked` is a YAML file at the root of the main worktree.

```yaml
version: 1
defaults:
  conflict: skip        # skip | overwrite | backup
  link: relative        # relative | absolute
rules:
  - path: .env
    action: copy
  - path: config/*.local.yml
    action: copy
  - path: certs/
    action: link
  - path: node_modules/
    action: clone
  - path: tmp/cache/
    action: link
    link: absolute      # per-rule override
```

`path` is relative to the worktree root and may contain glob patterns (`*`, `?`, `[...]`, `{a,b}`, `**`).
A trailing `/` restricts matches to directories.

| action | Result in the worktree | Suited for |
|---|---|---|
| `copy` | An independent copy | Files edited per worktree, such as `.env` |
| `link` | A symlink to the main worktree | Files that should be identical everywhere, such as certificates |
| `clone` | A copy-on-write copy, falling back to `copy` | Large directories such as `node_modules` |

`conflict` decides what happens when the destination already exists with different content.

| conflict | Behavior |
|---|---|
| `skip` | Leave it and report `differs` (default) |
| `overwrite` | Replace it |
| `backup` | Rename it to `<path>.orig` (`.orig.1`, `.orig.2`, ... if taken), then create |

Personal rules can be written in `.git/info/gituntracked` with the same format.
They are merged over the shared file, and a rule with the same `path` replaces the shared one.

Commands
--

```
git untracked init [--force]                  Create .gituntracked in the main worktree
git untracked sync [--all] [<worktree>...]    Apply the rules (default: current worktree)
git untracked status [--all] [<worktree>...]  Show the state of each rule
git untracked clean [--all] [<worktree>...]   Remove synced files that are still unmodified
git untracked install-hook [--force]          Sync automatically on `git worktree add`
git untracked uninstall-hook                  Remove the hook
git untracked version
```

`-n, --dry-run` shows what would be done, `-v, --verbose` also lists entries that need no change, and `sync --force` overwrites differing files regardless of `conflict`.

`status` reports one of the following states.

| state | Meaning |
|---|---|
| `ok` | As expected |
| `missing` | Not present in the worktree |
| `differs` | Present but different |
| `tracked` | Tracked in the worktree, so left alone |
| `nosource` | Nothing matches in the main worktree |

Hook
--

`install-hook` appends a marked block to `post-checkout` in `core.hooksPath` or `.git/hooks`.
Git runs `post-checkout` with an all-zero previous HEAD only when a worktree is created, so ordinary checkouts do not trigger a sync, and a failed sync never makes `git worktree add` fail.

When husky or lefthook is detected, `install-hook` prints the snippet to add to their configuration instead.
Use `--force` to install into the hooks directory anyway.

License
--

[MIT](LICENSE)
