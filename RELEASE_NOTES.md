# mdnav release notes

Release changes are recorded under their exact version, newest first.

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
