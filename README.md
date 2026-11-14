# mdnav

Ask for a thing in a set of Markdown documents by name and get the smallest
complete block that contains it, with file and line numbers, and nothing else.
It is built for coding agents that read binding design documents.

```
$ mdnav get D4 internal/nav/testdata
// internal/nav/testdata/01-register.md:10 (§ 01. Register > table row D4)
| ID | Decision | Source |
| D4 | Limits apply per lane. | Owner |

// see also (headings and other table rows with this id):
//   internal/nav/testdata/05-engine.md:15 ### Fence (D4)
```

## Commands

```
mdnav get <query> [paths...] [--limit N] [--max-lines N] [--regex] [--json] [--files-only]
mdnav outline <file> [--depth N] [--max-lines N] [--json]
mdnav refs <id> [paths...] [--include-def] [--full] [--limit N] [--json]
mdnav status <id> [paths...] [--full] [--limit N] [--json]
mdnav ids [paths...] [--pattern NAME] [--limit N] [--json]
```

Paths default to the current directory, searched recursively for `*.md`,
skipping `.git` and (inside a git work tree) git-ignored paths. Flags may come
before or after arguments; `--` ends flags. Every command also takes
`--config FILE` and `--timeout D` (default 30s).

Exit status: 0 on success, 1 when nothing matched, 2 on usage errors or failure.

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
listed at the end unless `--include-def`.

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
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./...
```

Layout: `cmd/mdnav` parses flags; `internal/markdown` parses documents,
`internal/idpat` matches ids, `internal/config` loads `.mdnav.json`,
`internal/source` finds and reads files, `internal/nav` answers queries,
`internal/render` prints text or JSON, `internal/app` wires them together.
