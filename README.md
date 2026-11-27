# mdnav

Ask for a thing in a set of Markdown documents by name and get the smallest
complete block that contains it, with file and line numbers, and nothing else.
It is built for coding agents that read binding design documents.

```
$ mdnav get D4 internal/nav/testdata
// internal/nav/testdata/01-register.md:10 (§ 01. Register > table row D4) #3a5aa99132bf
| ID | Decision | Source |
| D4 | Limits apply per lane. | Owner |

// see also (headings and other table rows with this id):
//   internal/nav/testdata/05-engine.md:15 #2dda930005e1 ### Fence (D4)
```

The `#3a5aa99132bf` after each location is the block's hash: the first 12 hex
digits of the SHA-256 of the block's lines (joined by `\n`), so it changes only
when the block's text changes, not when lines move or line endings differ. It
covers the whole block even when the output is cut or replaced by an outline.
Edit commands accept it as `--expect` to refuse writing over a block that
changed since it was read.

## Install

```
brew install --cask ozgurcd/tap/mdnav
# or: go install github.com/ozgurcd/mdnav/cmd/mdnav@latest
mdnav --version
```

## Commands

```
mdnav get <query> [paths...] [--limit N] [--max-lines N] [--regex] [--json] [--files-only]
mdnav outline <file> [--depth N] [--max-lines N] [--json]
mdnav refs <id> [paths...] [--include-def] [--full] [--limit N] [--json]
mdnav status <id> [paths...] [--full] [--limit N] [--json]
mdnav ids [paths...] [--pattern NAME] [--limit N] [--json]
mdnav version [--json]
mdnav sub | set | replace | insert | delete | add-row | apply   (see Editing)
```

Paths default to the current directory, searched recursively for `*.md`,
skipping `.git` and (inside a git work tree) git-ignored paths. Flags may come
before or after arguments; `--` ends flags. Every command also takes
`--config FILE` and `--timeout D` (default 30s).

Exit status: 0 on success, 1 when nothing matched, 2 on usage errors or failure,
3 when an edit's `--expect` or `--old` no longer matches the file.

### get

Resolution order; the first step that finds anything wins:

1. **Exact id**: the table row whose first cell is the id, printed with the
   table's header row. When several tables have such a row, the one in the
   table that defines the most ids of that pattern (the register) wins; the
   others, and headings that carry the id, are listed as one-line pointers.
   Without a defining row, headings that carry the id; without those, the
   blocks that mention it.
2. **§ number** (`§5.4` or `5.4`): the section whose heading starts with that
   number, up to the next heading of the same or higher level.
3. **Heading text** (case-insensitive substring): that section.
4. **Free text** (case-insensitive substring, or `--regex`, which skips steps
   1-3): the smallest enclosing block of each match: table row (with header
   row), list item (with nested lines), paragraph, blockquote or fenced code.
   File order, cut at `--limit` (default 10) with `... N more matches`.

A whole file is never printed. A section longer than `--max-lines` (default
200), or one that spans its whole file, is replaced by its outline and line
range, with a note. A longer block is cut around the match, with a note.

### outline

Heading tree with § numbers, line ranges, and the tables and table body rows
directly in each section.

### refs

Every line outside fenced code that mentions the id, grouped by file, with
the enclosing heading path. Long lines are shown as a window around the id,
marked with `…` (`--full` prints whole lines). Defining rows are left out and
listed at the end unless `--include-def`. Each line carries the hash of the
block an edit at that line would target (`359 #3a5aa99132bf: [§ …] …`); for a
heading line that is its whole section. `status` hits carry it the same way.

### status

A hint, never a verdict. Lines count when the id is struck through
(`~~...~~`), when the id's own row says "superseded", when the id follows
"supersede(s)", or when it shares a line with "superseded" outside a
"superseded (by ...)" clause that names it as the superseder. Output is
`possibly superseded: see lines ...` with the superseding id where one is on
the same line, or `active (no supersession text found)`.

### ids

Every identifier found, with defining locations, mention count and file
count, sorted by pattern and then naturally (D2 before D10).

## Editing

Edits address one block exactly like `get` (id, § number, heading text, free
text) or as `path:line`; a heading line addresses its whole section. A target
that matches more than one block is refused and the candidates are listed.
Edits write the file unless `--dry-run`, print a `-`/`+` diff and the block's
new `#hash`, and exit 3 when `--expect` or `--old` no longer matches.

```
mdnav sub <target> <old> <new> [paths...] [--expect H]
mdnav set <target> <value> --col HEADER [--old V] [--expect H] [paths...]
mdnav replace <target> --from FILE|- (--expect H | --force) [paths...]
mdnav insert (--before|--after) <target> --from FILE|- [--expect H] [paths...]
mdnav delete <target> (--expect H | --force) [paths...]
mdnav add-row <target> --cells 'a|b|c' [--expect H] [paths...]
mdnav apply EDITS.json|-
```

- `sub` is the in-place edit: `<old>` must occur exactly once in the block,
  otherwise nothing changes. It may span lines. Text shaped like a flag
  (`-x`) goes after `--`, which ends the flags: `mdnav sub -- D4 -x -y`;
  text such as `-> done` needs nothing.
- `set` changes one table cell, chosen by its column header, and keeps the
  cell's padding; a `|` in the value is escaped. With `--old` it fails unless
  the cell still holds that value. The target must be a table row: address
  it by its leading cells, as in `"| D4 |"`; a target that resolves to a
  heading is refused with that form.
- `replace` and `delete` overwrite a whole block, so they need the block's
  `--expect <hash>` from `get` or an explicit `--force`. A deleted block that
  sat between two blank lines takes one of them along.
- `insert` adds lines before or after a block, as given (start with an empty
  line to begin a new paragraph). After a section they go after its last
  line, before the blank line that precedes the next heading. Next to a table
  row they must be rows of that table.
- `add-row` appends a row to the table the target is in (a row of it, or a
  section holding exactly one table); the cell count must match the header.
- `apply` reads `{"file": "...", "edits": [...]}`, where each edit is
  `{"op": "sub", "target": "D4", "old": "...", "new": "...", "expect": "..."}`
  (ops: `sub`, `set`, `replace`, `insert-before`, `insert-after`, `delete`,
  `add-row`; fields `column`, `value`, `lines`, `cells`, `force`; unknown
  fields are rejected). It applies them in order under one lock and writes
  once or not at all.

The file is locked, read again and the block found again before anything is
written. The new contents go to a temporary file in the same directory, which
is synced and renamed over the original, so readers never see half a file and
two mdnav edits never lose each other's changes. Bytes outside the block stay
exactly as they were, including CRLF line endings, a missing final newline and
the file mode. Symbolic links are refused, and so are edits that would leave a
table row with the wrong number of cells or a code fence unclosed. An editor
that takes no lock can still race in the moment between the re-read and the
rename.

```
$ mdnav sub D4 'per lane' 'per host' internal/nav/testdata
// internal/nav/testdata/01-register.md:10 (§ 01. Register > table row D4) #1a5f4e8e838f  [sub of #3a5aa99132bf]
-| D4 | Limits apply per lane. | Owner |
+| D4 | Limits apply per host. | Owner |
// written
```

## Agent hook

`mdnav hook` is a PreToolUse hook that makes coding agents use mdnav for
Markdown. `--agent claude|codex|opencode|agy` (default `claude`) picks the
agent: a read layer per agent turns its input into one common event, the
rules below judge it, and a write layer per agent answers in that agent's
format. An unknown agent exits 2. An agent whose format has no recorded
fixtures yet exits 2 with `no fixture-backed format for <agent>`; only
`claude` is fixture-backed today. It denies, with the mdnav command to run
instead:

- shell commands that read a `.md` or `.markdown` file (`cat`, `head`, `tail`,
  `less`, `more`, `nl`, `sed`, `awk`, `cut`, `perl`), search one (`grep`,
  `rg`, `ag`, `git grep` on a Markdown file, glob, `--include`, `-g`, `-t md`,
  or a recursive search of a directory that holds Markdown) or write an
  existing one (`sed -i`, `perl -i`, `tee`, `>`, `>>`, `>|`, `&>`, `cp`, `mv`,
  `install`, `dd of=`, `truncate`), including inside `$(...)`, `bash -c`,
  `find -exec` and `xargs`;
- edit and multi-edit of an existing Markdown file, and write over one. The
  denial gives the exact `mdnav sub` commands when the edit maps to them,
  checked with a dry run first;
- `apply_patch` (Codex, opencode) that updates an existing Markdown file, adds
  one over an existing file or moves a file onto one.

It allows counts (`grep -c`, `wc`, output piped to `wc`), checksums, `stat`,
`ls`, `find` listings, `git diff`, `git show`, `git log`, Markdown names that
appear only in quoted text or here-document data, creating or deleting a
Markdown file, mdnav itself, and every path that is not Markdown. A command
carrying `# mdnav-cannot: <specific reason>` (three words or more) passes. A
shell or write call whose input cannot be read is denied, naming the tool; an
unknown read-only tool passes.

`MDNAV_HOOK_MODE=warn` (the default) allows every call and logs what would be
denied; `deny` denies. `MDNAV_CANNOT_LOG=FILE` appends one tab-separated line
per escape and per warn-mode denial: time, cwd, reason, command, agent.

`mdnav hook --record DIR` writes each raw input to `DIR/<agent>-<time>.json`
(file 0600, directory 0700; file content cut to 4 KiB, environment values
dropped, token-like strings redacted) and allows the call.
The recordings become fixtures in `<fixtures>/<agent>/`, each with a
`<name>.want` file holding `<verdict> <tool_kind>`. `mdnav hook --selftest
[--fixtures DIR]` runs the decision table for every agent, running each
command a denial names in a scratch fixture, then replays the fixtures.
`mdnav hook --write-fixture DIR` writes the tree to record in.

`mdnav hook --help` prints the registration for each agent. For Claude Code:

```json
{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "mdnav hook --agent claude"}]},
      {"matcher": "Edit|Write|MultiEdit", "hooks": [{"type": "command", "command": "mdnav hook --agent claude"}]}
    ]
  }
}
```

## Identifier patterns

Built in: `D\d+`, `V\d+`, `FR\d+`, `NFR\d+`, `GF-\d+`, `FP-[A-Z]+-\d+`,
`FT-\d+`, `M\d+(\.[A-Z]+-\d+)?`, `M-[A-Z]+-\d+`, `Q-[A-Z]+-\d+`, `RT-Q\d+`,
`C\d{2}`, `T\d{2}`, `PQ-[A-Z0-9-]+`. Ids match on word boundaries, so `D33`
never matches inside `D333`.

Override them with `.mdnav.json` in the working directory or a parent (or
`--config`). Unknown fields are rejected.

```json
{
  "patterns":       [{"name": "D", "regex": "D\\d+"}],
  "extra_patterns": [{"name": "ADR", "regex": "ADR-\\d+"}]
}
```

`patterns` replaces the built-in set; `extra_patterns` is appended.

## Parser scope

Line-based: ATX headings, pipe tables (escaped `\|` stays in its cell), list
items nested by indentation, paragraphs, fenced code (``` and ~~~; skipped by
id and heading matching, searched by free text), blockquotes, `~~strike~~`.
LF and CRLF. Not CommonMark; nothing is rendered.

## No index

Every call parses the files; nothing is cached, so nothing goes stale.
Measured on a 48-file, 18,285-line design corpus (4 CPUs, 2026-11-14):
`get` 20-37 ms, `refs` 36-45 ms, `status` 27 ms, `ids --limit 0` 130 ms.
All are under the 200 ms threshold for adding an index.

## Build

Go 1.27.4, standard library only.

```
CGO_ENABLED=0 go build ./cmd/mdnav
make verify          # fmt, test, vet, staticcheck, govulncheck, mod tidy
```

Layout: `cmd/mdnav` parses flags; `internal/markdown` parses documents,
`internal/idpat` matches ids, `internal/config` loads `.mdnav.json`,
`internal/source` finds and reads files, `internal/nav` answers queries and
resolves edit targets, `internal/edit` applies edits, `internal/fileio` locks
and atomically rewrites files, `internal/render` prints text or JSON, and
`internal/app` wires them together.

## Release

1. Set `internal/buildinfo.Version` and add a `## vX.Y.Z — YYYY-MM-DD`
   section to `RELEASE_NOTES.md`; merge to main.
2. Push an annotated tag on that commit: `git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`.

`.github/workflows/release.yml` then checks the tag against the source
version (tests are not re-run: the commit passed them on its pull request),
publishes the GitHub release with GoReleaser
(darwin and linux, amd64 and arm64), and pushes the cask to
`ozgurcd/homebrew-tap` as `Casks/mdnav.rb`. It needs the repository secret
`HOMEBREW_TAP_GITHUB_TOKEN` with push access to the tap.

## Licence

MIT. See [LICENSE](LICENSE).
