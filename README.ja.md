<p align="right"><a href="https://github.com/linyows/git-untracked/blob/main/README.md">English</a> | 日本語</p>

<br><br><br><br>
<h1>git-untracked</h1>

  <strong>git-untracked</strong>は、untrackedなファイルをすべての<a href="https://git-scm.com/docs/git-worktree">git worktree</a>へ届けるgitサブコマンドです。
<br><br><br><br>

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

`git worktree add`で作ったworktreeにはtrackedなファイルしかありません。
そのため、`.env`や開発用の証明書、`node_modules`を、worktreeを作るたびに用意し直すことになります。
git-untrackedは、`.gituntracked`に書いたルールに従って、これらをmain worktreeからコピー、シンボリックリンク、クローンします。
`git worktree add`の実行時に、自動で同期させることもできます。

特徴
--

- パスごとに`copy`、`link`（シンボリックリンク）、`clone`（Copy-on-Write）の3つから動作を選べます
- `clone`は、macOS（APFS）では`clonefile(2)`、Linux（Btrfs、XFS）では`FICLONE`を使います。対応していないファイルシステムでは通常のコピーに切り替えます
- `post-checkout` hookにより、`git worktree add`の実行時に自動で同期します
- `**`を含むglobパターンを使えます
- リポジトリにコミットする共通のルールに加えて、`.git/info`に個人用のルールを置けます
- trackedなファイルは上書きしません。シンボリックリンクになったディレクトリを経由して、main worktreeへ書き込むこともしません
- `clean`は、main worktreeと内容が一致しているものだけを削除します

インストール
--

```sh
brew install linyows/git-untracked/git-untracked
```

または

```sh
go install github.com/linyows/git-untracked/cmd/git-untracked@latest
```

macOSとLinuxに対応しています。

使い方
--

```sh
cd path/to/repo              # main worktree
git untracked init           # .gituntracked を作成
$EDITOR .gituntracked
git untracked install-hook   # git worktree add のたびに同期する

git worktree add ../feature -b feature
# ../feature に .env、certs/、node_modules/ が用意される
```

設定
--

`.gituntracked`は、main worktreeのルートに置くYAMLファイルです。

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
    link: absolute      # ルールごとに上書きできる
```

`path`はworktreeのルートからの相対パスで、globパターン（`*`、`?`、`[...]`、`{a,b}`、`**`）を使えます。
末尾に`/`を付けると、ディレクトリにだけマッチします。

| action | worktreeにできるもの | 向いている用途 |
|---|---|---|
| `copy` | 独立したコピー | `.env`のように、worktreeごとに編集するファイル |
| `link` | main worktreeへのシンボリックリンク | 証明書のように、どのworktreeでも同じでよいファイル |
| `clone` | Copy-on-Writeのコピー（非対応なら`copy`） | `node_modules`のような大きなディレクトリ |

`conflict`は、同期先にすでに異なる内容のファイルがある場合の動作を決めます。

| conflict | 動作 |
|---|---|
| `skip` | そのまま残し、`differs`として報告します（デフォルト） |
| `overwrite` | 置き換えます |
| `backup` | `<path>.orig`に退避してから作ります（使用済みなら`.orig.1`、`.orig.2`…） |

個人用のルールは、同じ書式で`.git/info/gituntracked`に書けます。
共通のルールに重ねてマージされ、同じ`path`のルールは個人用のものが優先されます。

コマンド
--

```
git untracked init [--force]                  main worktreeに.gituntrackedを作成
git untracked sync [--all] [<worktree>...]    ルールを適用（デフォルト: カレントworktree）
git untracked status [--all] [<worktree>...]  各ルールの状態を表示
git untracked clean [--all] [<worktree>...]   同期したもののうち、変更されていないものを削除
git untracked install-hook [--force]          git worktree add の実行時に自動で同期
git untracked uninstall-hook                  hookを削除
git untracked version
```

`-n, --dry-run`を付けると、変更せずに実行内容だけを表示します。
`-v, --verbose`を付けると、変更の必要がない項目も表示します。
`sync --force`は、`conflict`の設定に関わらず、異なるファイルを上書きします。

`status`は、次のいずれかの状態を表示します。

| 状態 | 意味 |
|---|---|
| `ok` | 期待どおりの状態です |
| `missing` | worktreeに存在しません |
| `differs` | 存在しますが、内容が異なります |
| `tracked` | worktreeでtrackedなので、対象外です |
| `nosource` | main worktreeにマッチするものがありません |

hook
--

`install-hook`は、`core.hooksPath`または`.git/hooks`にある`post-checkout`へ、マーカーで囲んだブロックを追記します。
gitが`post-checkout`に渡す旧HEADがすべて0になるのは、worktreeを作ったときだけです。
そのため、通常のcheckoutでは同期は実行されません。
同期に失敗しても、`git worktree add`自体は失敗しません。

huskyやlefthookを検出した場合、`install-hook`はhookを設置せず、それぞれの設定に追加する内容を表示します。
`--force`を付けると、hookのディレクトリへそのまま設置します。

ライセンス
--

[MIT](LICENSE)
