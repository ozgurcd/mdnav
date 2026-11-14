# mdnav release notes

Release changes are recorded under their exact version, newest first.

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
