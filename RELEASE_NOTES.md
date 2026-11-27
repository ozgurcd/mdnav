# mdnav release notes

Release changes are recorded under their exact version, newest first.

## v0.1.3 — 2026-11-27

`mdnav hook` now guards Codex, opencode and Antigravity as well as Claude Code.

- `mdnav hook --agent claude|codex|opencode|agy` reads each agent's hook
  input and answers in its format; the Markdown rules are the same for all.
  Every agent is backed by calls recorded in real sessions.
- `mdnav hook --print-install --agent X` prints the complete registration
  (warn mode, logging to `~/.local/state/mdnav/X-mdnav-cannot.log`), naming
  the mdnav binary by absolute path. For opencode 2 it prints `mdnav-guard.js`,
  a plugin for `~/.config/opencode/plugins/`.
- `mdnav hook --check-install --agent X [--json]` reports whether the mdnav
  guard is registered, missing or different.
- Antigravity CLI's `edit_file` and `write_file` are guarded, besides the
  documented `replace_file_content`, `multi_replace_file_content` and
  `write_to_file`.
- `mdnav hook --record DIR` (opt-in) writes raw hook inputs for new fixtures;
  `--selftest --fixtures DIR` replays them.

## v0.1.2 — 2026-11-26

mdnav now ships an agent hook that sends coding agents to mdnav for Markdown.

- New `mdnav hook`: a Claude Code PreToolUse hook. It reads the hook JSON on
  standard input and denies Bash commands that read (`cat`, `head`, `tail`,
  `less`, `more`, `nl`, `sed`, `awk`), search (`grep`, `rg`, `ag`,
  `git grep`) or write (`sed -i`, `perl -i`, `tee`, `>`, `>>`, `>|`, `&>`,
  `cp`, `mv`, `install`, `dd of=`, `truncate`) a `.md` or `.markdown` file,
  and Edit, MultiEdit or Write calls that change an existing one. Each denial
  names a mdnav command that works as written; Edit and `sed -i` replacements
  become exact `mdnav sub` commands, checked with a dry run.
- Counts, checksums, listings, `git diff`/`show`/`log`, quoted text,
  here-document data, new files and mdnav itself pass. A command carrying
  `# mdnav-cannot: <specific reason>` passes and is logged to
  `MDNAV_CANNOT_LOG`. `MDNAV_HOOK_MODE=warn` (the default) allows and logs
  what `deny` mode would deny. `mdnav hook --selftest` runs the decision
  table.
- An argument that starts with `-` but is not shaped like a flag (`->`,
  `-> done`, `-1`) is taken as an argument; flag-shaped text goes after `--`,
  and the error says so.
- `set` on a heading or other non-row block names the row form to use: the
  exact `"| name |"` when a row with that first cell exists.

## v0.1.1 — 2026-11-23

mdnav can now change the documents it navigates, one block at a time.

- New edit commands, each addressing one block the way `get` does (id, §
  number, heading, free text) or as `path:line`; an ambiguous target is
  refused with its candidates:
  - `sub`: in-place edit; replaces the one occurrence of an exact text in the
    block.
  - `set`: sets one table cell by column header (`--old` guards the current
    value).
  - `replace`, `delete`: overwrite or remove a whole block; they need
    `--expect <hash>` or `--force`.
  - `insert --before|--after`: adds lines next to a block.
  - `add-row`: appends a row to a table, checking the cell count.
  - `apply`: several edits to one file from a JSON document, in order, all or
    nothing.
- Every edit takes `--dry-run` and `--json`, prints a `-`/`+` diff and the
  block's new hash, and exits 3 when `--expect` or `--old` no longer matches.
- Safe writes: the file is locked, re-read and the block found again before
  writing; the result is written to a temporary file and renamed over the
  original. Bytes outside the block, CRLF endings, a missing final newline and
  the file mode are kept. Symbolic links, a table row with the wrong number of
  cells and an unclosed code fence are refused.
- Every block printed by `get`, every `refs` line and every `status` hit
  carries a 12-hex-digit block hash (`#3a5aa99132bf`, JSON field `hash`). It
  depends only on the block's text and is the value edits take as `--expect`.
- `get` accepts `path:line` and returns the block at that line (a heading line
  gives its section).

## v0.1.0 — 2026-11-14

First release.

- `get`: exact id (defining table row with its header row), § number,
  heading text, then free text, returning the smallest enclosing block with
  `path:start-end`. Long sections show their outline; long blocks are cut
  around the match, always with a note.
- `outline`, `refs`, `status` (supersession hint, never a verdict) and `ids`.
- Built-in id patterns, overridable with `.mdnav.json` (unknown fields
  rejected). `--json` on every command.
- Homebrew cask in `ozgurcd/homebrew-tap`: `brew install --cask ozgurcd/tap/mdnav`.
