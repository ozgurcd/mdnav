package hook

import (
	"slices"
	"strings"
)

// opts holds parsed command options.
type opts struct {
	short map[byte][]string // letter: values ("" for a flag)
	long  map[string][]string
	args  []string // operands in order
	dash  int      // index in args of the first operand after "--", -1 when none
}

func (o opts) has(short string, long ...string) bool {
	for i := range len(short) {
		if _, ok := o.short[short[i]]; ok {
			return true
		}
	}
	for _, l := range long {
		if _, ok := o.long[l]; ok {
			return true
		}
	}
	return false
}

func (o opts) values(short byte, long ...string) []string {
	out := slices.Clone(o.short[short])
	for _, l := range long {
		out = append(out, o.long[l]...)
	}
	return out
}

// parseOpts parses words with short clusters (-rn, -A3, -e PAT) and long
// options (--include=X, --include X), given the short letters and long names
// that take a value.
func parseOpts(words []string, shortValue string, longValue ...string) opts {
	o := opts{short: map[byte][]string{}, long: map[string][]string{}, dash: -1}
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "--":
			o.dash = len(o.args)
			o.args = append(o.args, words[i+1:]...)
			return o
		case strings.HasPrefix(w, "--"):
			name, v, eq := strings.Cut(w[2:], "=")
			if !eq && slices.Contains(longValue, name) && i+1 < len(words) {
				v = words[i+1]
				i++
			}
			o.long[name] = append(o.long[name], v)
		case len(w) > 1 && w[0] == '-':
			for j := 1; j < len(w); j++ {
				c := w[j]
				if strings.IndexByte(shortValue, c) < 0 {
					o.short[c] = append(o.short[c], "")
					continue
				}
				v := w[j+1:]
				if v == "" && i+1 < len(words) {
					v = words[i+1]
					i++
				}
				o.short[c] = append(o.short[c], v)
				break
			}
		default:
			o.args = append(o.args, w)
		}
	}
	return o
}

// scope is what a search's file filters say about Markdown.
type scope struct{ mdIn, otherIn, mdOut bool }

func (s *scope) include(glob string) {
	if isMD(glob) || mentionsMD(glob) || glob == "md" || glob == "markdown" {
		s.mdIn = true
	} else {
		s.otherIn = true
	}
}

func (s *scope) exclude(glob string) {
	if isMD(glob) || mentionsMD(glob) || glob == "md" || glob == "markdown" {
		s.mdOut = true
	}
}

// coversMD reports whether a search over directories reaches Markdown files.
func (s scope) coversMD() bool { return !s.mdOut && (s.mdIn || !s.otherIn) }

// search judges grep, egrep, fgrep, rg, ag and git grep.
func (b *bash) search(prog string, args []string, piped bool, inputs []string, cwd string) {
	var o opts
	var sc scope
	f := finding{rule: ruleSearch, prog: prog, syntax: "bre"}
	recursive, stdinWhenNoPath := false, false
	switch prog {
	case "grep", "egrep", "fgrep":
		o = parseOpts(args, "efmABCdD", "regexp", "file", "max-count", "after-context", "before-context",
			"context", "include", "exclude", "exclude-dir", "exclude-from", "label", "devices", "directories", "binary-files")
		if o.has("cq", "count", "quiet", "silent") {
			return
		}
		recursive = o.has("rR", "recursive", "dereference-recursive") || slices.Contains(o.values('d', "directories"), "recurse")
		switch {
		case prog == "fgrep" || o.has("F", "fixed-strings"):
			f.syntax = "fixed"
		case prog == "egrep" || o.has("EP", "extended-regexp", "perl-regexp"):
			f.syntax = "ere"
		}
		for _, g := range o.values(0, "include") {
			sc.include(g)
		}
		for _, g := range o.values(0, "exclude") {
			sc.exclude(g)
		}
		f.hint = "--exclude='*.md'"
	case "rg":
		o = parseOpts(args, "efgtTmABCMjrEd", "regexp", "file", "glob", "iglob", "type", "type-not", "max-count",
			"after-context", "before-context", "context", "max-columns", "threads", "replace", "encoding", "max-depth",
			"type-add", "type-clear", "sort", "sortr", "colors", "context-separator", "field-match-separator",
			"path-separator", "pre", "pre-glob", "ignore-file", "max-filesize", "dfa-size-limit", "regex-size-limit", "engine")
		if o.has("cq", "count", "count-matches", "quiet", "files", "type-list") {
			return
		}
		recursive, stdinWhenNoPath, f.syntax = true, true, "ere"
		if o.has("F", "fixed-strings") {
			f.syntax = "fixed"
		}
		for _, g := range o.values('g', "glob", "iglob") {
			if rest, ok := strings.CutPrefix(g, "!"); ok {
				sc.exclude(rest)
			} else {
				sc.include(g)
			}
		}
		for _, t := range o.values('t', "type") {
			sc.include(t)
		}
		for _, t := range o.values('T', "type-not") {
			sc.exclude(t)
		}
		f.hint = "-g '!*.md'"
	case "ag":
		o = parseOpts(args, "GABCmpgW", "file-search-regex", "after", "before", "context", "max-count", "ignore",
			"ignore-dir", "path-to-ignore", "depth", "pager", "workers", "width")
		if o.has("cg", "count") {
			return
		}
		recursive, stdinWhenNoPath, f.syntax = true, true, "ere"
		if o.has("Q", "literal") {
			f.syntax = "fixed"
		}
		for _, g := range o.values('G', "file-search-regex") {
			sc.include(g)
		}
		if o.has("", "md", "markdown") {
			sc.mdIn = true
		}
		for _, g := range o.values(0, "ignore") {
			sc.exclude(g)
		}
		f.hint = "--ignore '*.md'"
	case "git grep":
		o = parseOpts(args, "efABCmO", "regexp", "after-context", "before-context", "context", "max-count",
			"max-depth", "threads")
		if o.has("cq", "count", "quiet") {
			return
		}
		recursive = true
		switch {
		case o.has("F", "fixed-strings"):
			f.syntax = "fixed"
		case o.has("EP", "extended-regexp", "perl-regexp"):
			f.syntax = "ere"
		}
		f.hint = "-- ':!*.md'"
	}

	f.patterns = o.values('e', "regexp")
	operands, shift := o.args, 0
	if len(f.patterns) == 0 && !o.has("f", "file") && len(operands) > 0 && (o.dash != 0 || prog != "git grep") {
		f.patterns, operands, shift = operands[:1], operands[1:], 1
	}

	if prog == "git grep" {
		// Operands before "--" are revisions unless they exist or look like
		// pathspecs; operands after it are pathspecs.
		var specs []string
		for i, a := range operands {
			if (o.dash >= 0 && i+shift >= o.dash) || strings.HasPrefix(a, ":") || strings.ContainsAny(a, "*?[") || b.isDir(cwd, a) || isMD(a) {
				specs = append(specs, a)
			}
		}
		operands = nil
		for _, s := range specs {
			if rest, ok := cutExclude(s); ok {
				sc.exclude(rest)
				continue
			}
			if strings.ContainsAny(s, "*?[") && !isMD(s) {
				sc.include(s)
				continue
			}
			if isMD(s) && strings.ContainsAny(s, "*?[") {
				sc.mdIn = true // git globs match in every directory
				continue
			}
			operands = append(operands, s)
		}
	}

	operands = append(operands, inputs...)
	var dirs []string
	for _, a := range operands {
		switch {
		case isMD(a):
			f.files = append(f.files, a)
			f.paths = append(f.paths, a)
		case recursive && b.isDir(cwd, a):
			dirs = append(dirs, a)
		}
	}
	if recursive && len(operands) == 0 {
		switch {
		case stdinWhenNoPath && piped: // rg and ag search the pipe
		case prog == "git grep" || stdinWhenNoPath:
			dirs = []string{""} // the whole tree, which is mdnav's default too
		default:
			dirs = []string{"."} // grep -r with no path searches the working directory
		}
	}
	if sc.coversMD() {
		for _, d := range dirs {
			if !hasMD(resolve(cwd, d)) {
				continue // no Markdown below it: not this hook's business
			}
			f.recursive = true
			if d != "" {
				f.paths = append(f.paths, d)
			}
		}
	}
	if len(f.files) > 0 || f.recursive {
		b.add(f)
	}
}

// cutExclude returns the pattern of an excluding git pathspec.
func cutExclude(s string) (string, bool) {
	for _, p := range []string{":!", ":^", ":(exclude)"} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			return rest, true
		}
	}
	return "", false
}
