# mdnav release notes

Release changes are recorded under their exact version, newest first.

## Unreleased

- Every block printed by `get`, every `refs` line and every `status` hit
  carries a 12-hex-digit block hash (`#3a5aa99132bf`, JSON field `hash`). It
  depends only on the block's text and is the value future edit commands take
  as `--expect`.

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
