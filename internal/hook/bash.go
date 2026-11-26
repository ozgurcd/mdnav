package hook

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ozgurcd/mdnav/internal/shell"
)

// Rules, in the order a denial prefers them when one command breaks several.
const (
	ruleRead      = "read"
	ruleSearch    = "search"
	ruleWrite     = "write"
	ruleEdit      = "edit"
	ruleOverwrite = "overwrite"
)

// finding is one Markdown access that mdnav should make instead.
type finding struct {
	rule  string
	prog  string   // the program, or the redirection operator
	files []string // Markdown files as written
	line  int      // read: the first line the command names, 0 when none

	// search
	patterns  []string
	syntax    string   // fixed, bre, ere (ERE, PCRE and Rust regex alike)
	paths     []string // Markdown files and directories searched
	recursive bool     // it searched directories
	hint      string   // how to keep this search off Markdown

	// write: sed -i 's/old/new/' with literal text
	sedOld, sedNew string
	sedSub         bool
}

func rank(rule string) int {
	switch rule {
	case ruleRead:
		return 1
	case ruleSearch:
		return 2
	}
	return 3
}

// bash judges a shell command line run in cwd.
type bash struct {
	root  string // the agent's cwd
	cwd   string // where the current command runs
	cd    string // that directory as the agent would name it from its cwd; "" for the cwd itself
	found []finding
}

// chdir follows "cd DIR", so later commands are judged where they run and
// their findings name paths from the agent's cwd.
func (b *bash) chdir(args []string) {
	if len(args) != 1 || strings.ContainsAny(args[0], "$`*?") || args[0] == "-" {
		return
	}
	b.cwd = resolve(b.cwd, args[0])
	if filepath.IsAbs(args[0]) || strings.HasPrefix(args[0], "~") {
		b.cd = b.cwd
	} else {
		b.cd = filepath.Join(b.cd, args[0])
	}
}

// fromCwd names a path the current command uses as the agent's cwd would.
func (b *bash) fromCwd(p string) string {
	if b.cd == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return p
	}
	return filepath.Join(b.cd, p)
}

func (b *bash) script(sc shell.Script, depth int) {
	for i, c := range sc.Commands {
		var feed []string // Markdown names the previous pipeline stage produced
		if c.Piped && i > 0 {
			feed = mdFeed(sc.Commands[i-1])
		}
		counted := i+1 < len(sc.Commands) && sc.Commands[i+1].Piped && isCounter(sc.Commands[i+1])
		before := len(b.found)
		b.command(c, feed, depth)
		if counted { // its output only feeds a count or checksum
			kept := b.found[:before]
			for _, f := range b.found[before:] {
				if f.rule == ruleWrite {
					kept = append(kept, f)
				}
			}
			b.found = kept
		}
	}
}

// add records a finding. A write counts only for files that exist: creating
// a new Markdown file is allowed, as with the Write tool.
func (b *bash) add(f finding) {
	if f.rule == ruleWrite {
		var existing []string
		for _, p := range f.files {
			if b.exists(p) {
				existing = append(existing, p)
			}
		}
		if len(existing) == 0 {
			return
		}
		f.files = existing
	}
	if b.cd != "" {
		for i := range f.files {
			f.files[i] = b.fromCwd(f.files[i])
		}
		for i := range f.paths {
			f.paths[i] = b.fromCwd(f.paths[i])
		}
		if f.recursive && len(f.paths) == 0 {
			f.paths = []string{b.cd}
		}
	}
	b.found = append(b.found, f)
}

func (b *bash) command(c shell.Command, feed []string, depth int) {
	words := stripPrefix(c.Words)
	if len(words) > 0 && filepath.Base(words[0]) == "mdnav" {
		return // mdnav itself, whatever it is given
	}
	var inputs []string
	for _, r := range c.Redirs {
		switch r.Op {
		case ">", ">>", ">|", "&>", "&>>", ">&", "<>":
			if isMD(r.Target) {
				b.add(finding{rule: ruleWrite, prog: r.Op, files: []string{r.Target}})
			}
		case "<":
			if isMD(r.Target) {
				inputs = append(inputs, r.Target)
			}
		}
	}
	if len(words) == 0 {
		if len(inputs) > 0 { // a loop reading Markdown line by line: done < x.md
			b.add(finding{rule: ruleRead, prog: "<", files: inputs})
		}
		return
	}
	name, args := filepath.Base(words[0]), words[1:]
	switch name {
	case "cd", "pushd":
		b.chdir(args)
	case "sh", "bash", "zsh", "dash", "ksh":
		for i, a := range args {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c") && i+1 < len(args) {
				b.nested(args[i+1], depth)
				break
			}
		}
	case "eval":
		b.nested(strings.Join(args, " "), depth)
	case "cat", "tac", "nl", "less", "more", "most", "bat", "batcat", "head", "tail", "cut", "view":
		if files := append(mdOperands(args, readerValues[name]), inputs...); len(files) > 0 {
			b.add(finding{rule: ruleRead, prog: name, files: files})
		}
	case "sed", "gsed":
		b.sed(name, args, inputs)
	case "awk", "gawk", "mawk", "nawk":
		b.awk(name, args, inputs)
	case "perl":
		b.perl(args, inputs)
	case "grep", "egrep", "fgrep", "rg", "ag":
		b.search(name, args, c.Piped, inputs, b.cwd)
	case "git":
		b.git(args, c.Piped)
	case "tee":
		b.writes(name, mdOperands(args, ""))
	case "cp", "mv", "install":
		b.copyDest(name, args)
	case "dd":
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok && isMD(v) {
				b.add(finding{rule: ruleWrite, prog: name, files: []string{v}})
			} else if v, ok := strings.CutPrefix(a, "if="); ok && isMD(v) {
				b.add(finding{rule: ruleRead, prog: name, files: []string{v}})
			}
		}
	case "truncate":
		b.writes(name, mdOperands(args, "sr"))
	case "xargs":
		if len(feed) > 0 {
			b.onFound(xargsCommand(args), feed, depth)
		}
	case "find":
		b.find(args, depth)
	}
}

func (b *bash) nested(src string, depth int) {
	if depth >= 4 {
		return
	}
	if sc, err := shell.Parse(src); err == nil {
		b.script(sc, depth+1)
	}
}

func (b *bash) writes(prog string, files []string) {
	if len(files) > 0 {
		b.add(finding{rule: ruleWrite, prog: prog, files: files})
	}
}

// foundMD stands for the Markdown files find or xargs hand a command. Its
// glob makes it count as existing.
const foundMD = "{}*.md"

// onFound judges words that find -exec or xargs run on the Markdown files
// found under starts: as written, and when that finds nothing, with a
// Markdown file in place of {} or appended.
func (b *bash) onFound(words, starts []string, depth int) {
	var plain []string
	for _, w := range words {
		if w != "{}" {
			plain = append(plain, w)
		}
	}
	if len(plain) == 0 {
		return
	}
	before := len(b.found)
	b.command(shell.Command{Words: plain}, nil, depth)
	if len(b.found) > before {
		return
	}
	b.command(shell.Command{Words: append(plain, foundMD)}, nil, depth)
	for k := before; k < len(b.found); k++ {
		f := &b.found[k]
		f.files, f.paths = nil, starts
		f.recursive = f.rule == ruleSearch
	}
}

func (b *bash) copyDest(name string, args []string) {
	var ops []string
	target := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			ops = append(ops, args[i+1:]...)
			i = len(args)
		case a == "-t" && i+1 < len(args):
			target = args[i+1]
			i++
		case strings.HasPrefix(a, "--target-directory="):
			target = strings.TrimPrefix(a, "--target-directory=")
		case name == "install" && (a == "-m" || a == "-o" || a == "-g" || a == "-S") && i+1 < len(args):
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			ops = append(ops, a)
		}
	}
	if target != "" || len(ops) < 2 {
		return // into a directory: names, not contents, change
	}
	if dest := ops[len(ops)-1]; isMD(dest) {
		b.add(finding{rule: ruleWrite, prog: name, files: []string{dest}})
	}
}

var sedPrint = regexp.MustCompile(`^(\d+)(?:,\d+)?p$`)

func (b *bash) sed(name string, args, inputs []string) {
	var scripts, files []string
	inplace, hasScript := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			n, v, eq := strings.Cut(a[2:], "=")
			switch n {
			case "in-place":
				inplace = true
			case "expression", "file":
				hasScript = true
				if !eq && i+1 < len(args) {
					v = args[i+1]
					i++
				}
				if n == "expression" {
					scripts = append(scripts, v)
				}
			case "line-length":
				if !eq {
					i++
				}
			}
		case len(a) > 1 && a[0] == '-':
		cluster:
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'i':
					inplace = true
					break cluster
				case 'e', 'f', 'l':
					v := a[j+1:]
					if v == "" && i+1 < len(args) {
						v = args[i+1]
						i++
					}
					if a[j] != 'l' {
						hasScript = true
					}
					if a[j] == 'e' {
						scripts = append(scripts, v)
					}
					break cluster
				}
			}
		case !hasScript:
			scripts, hasScript = append(scripts, a), true
		default:
			files = append(files, a)
		}
	}
	md := append(filterMD(files), inputs...)
	if len(md) == 0 {
		return
	}
	if !inplace {
		f := finding{rule: ruleRead, prog: name, files: md}
		if len(scripts) == 1 {
			if m := sedPrint.FindStringSubmatch(strings.TrimSpace(scripts[0])); m != nil {
				f.line, _ = strconv.Atoi(m[1])
			}
		}
		b.add(f)
		return
	}
	f := finding{rule: ruleWrite, prog: name + " -i", files: md}
	if len(scripts) == 1 {
		f.sedOld, f.sedNew, f.sedSub = literalSub(scripts[0])
	}
	b.add(f)
}

// literalSub splits a sed script s/old/new/[g] whose old and new text are
// literal, so mdnav sub can do the same.
func literalSub(script string) (old, new string, ok bool) {
	if len(script) < 4 || script[0] != 's' {
		return "", "", false
	}
	d := string(script[1])
	parts := strings.Split(script[2:], d)
	if len(parts) != 3 || strings.Trim(parts[2], "g") != "" || parts[0] == "" {
		return "", "", false
	}
	if strings.ContainsAny(parts[0], `\.[]*^$+?(){}|`) || strings.ContainsAny(parts[1], `\&`) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (b *bash) awk(name string, args, inputs []string) {
	var files []string
	inplace, hasProg := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-i" && i+1 < len(args):
			inplace = inplace || args[i+1] == "inplace"
			i++
		case a == "--include=inplace" || a == "-iinplace":
			inplace = true
		case a == "-F" || a == "-v" || a == "-E":
			i++
		case a == "-f" && i+1 < len(args):
			hasProg = true
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		case !hasProg:
			hasProg = true
		case strings.Contains(a, "=") && !strings.Contains(a, "/"):
			// var=value assignment
		default:
			files = append(files, a)
		}
	}
	md := append(filterMD(files), inputs...)
	switch {
	case len(md) == 0:
	case inplace:
		b.add(finding{rule: ruleWrite, prog: name + " -i inplace", files: md})
	default:
		b.add(finding{rule: ruleRead, prog: name, files: md})
	}
}

func (b *bash) perl(args, inputs []string) {
	var files []string
	inplace, hasCode := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case len(a) > 1 && a[0] == '-' && a[1] != '-':
		cluster:
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'i':
					inplace = true
					break cluster
				case 'e', 'E':
					hasCode = true
					if j+1 == len(a) {
						i++
					}
					break cluster
				case 'I', 'M', 'm', 'F', 'x', 'C', 'l', '0', 'd', 'D':
					break cluster
				}
			}
		case strings.HasPrefix(a, "--"):
		case !hasCode:
			hasCode = true // the script file
		default:
			files = append(files, a)
		}
	}
	md := append(filterMD(files), inputs...)
	switch {
	case len(md) == 0:
	case inplace:
		b.add(finding{rule: ruleWrite, prog: "perl -i", files: md})
	default:
		b.add(finding{rule: ruleRead, prog: "perl", files: md})
	}
}

func (b *bash) git(args []string, piped bool) {
	cwd := b.cwd
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" && i+1 < len(args):
			cwd = resolve(cwd, args[i+1])
			i++
		case (a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace") && i+1 < len(args):
			i++
		case strings.HasPrefix(a, "-"):
		case a == "grep":
			b.search("git grep", args[i+1:], piped, nil, cwd)
			return
		default:
			return // any other git command: history and status mdnav cannot open
		}
	}
}

func (b *bash) find(args []string, depth int) {
	var starts []string
	i := 0
	for ; i < len(args) && !strings.HasPrefix(args[i], "-") && args[i] != "(" && args[i] != "!"; i++ {
		starts = append(starts, args[i])
	}
	if len(starts) == 0 {
		starts = []string{"."}
	}
	md := false
	for ; i < len(args); i++ {
		switch args[i] {
		case "-name", "-iname", "-path", "-ipath", "-wholename", "-regex", "-iregex":
			if i+1 < len(args) && mentionsMD(args[i+1]) {
				md = true
			}
		case "-exec", "-execdir", "-ok", "-okdir":
			var sub []string
			for i++; i < len(args) && args[i] != ";" && args[i] != "+"; i++ {
				sub = append(sub, args[i])
			}
			if md {
				b.onFound(sub, starts, depth)
			}
		}
	}
}

func xargsCommand(args []string) []string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-I" || a == "-n" || a == "-P" || a == "-L" || a == "-d" || a == "-s" || a == "-E":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return args[i:]
		}
	}
	return nil
}

// mdFeed returns the Markdown names a pipeline stage lists, such as
// find -name '*.md' or git ls-files '*.md'.
func mdFeed(c shell.Command) []string {
	words := stripPrefix(c.Words)
	if len(words) == 0 {
		return nil
	}
	for _, w := range words[1:] {
		if mentionsMD(w) {
			if filepath.Base(words[0]) == "find" && len(words) > 1 && !strings.HasPrefix(words[1], "-") {
				return []string{words[1]}
			}
			return []string{"."}
		}
	}
	return nil
}

func isCounter(c shell.Command) bool {
	words := stripPrefix(c.Words)
	if len(words) == 0 {
		return false
	}
	switch filepath.Base(words[0]) {
	case "wc", "shasum", "sha1sum", "sha256sum", "sha512sum", "md5", "md5sum", "cksum", "b2sum", "sum":
		return true
	}
	return false
}

// readerValues lists, per reader, the short options that take a value.
var readerValues = map[string]string{"head": "nc", "tail": "ncs", "cut": "dfbc", "nl": "bsvinwd", "bat": "lrHm", "batcat": "lrHm"}

// mdOperands returns the Markdown operands of a command whose short options
// in valued take a value.
func mdOperands(args []string, valued string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, filterMD(args[i+1:])...)
		case len(a) == 2 && a[0] == '-' && strings.IndexByte(valued, a[1]) >= 0:
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		case isMD(a):
			out = append(out, a)
		}
	}
	return out
}

func filterMD(words []string) []string {
	var out []string
	for _, w := range words {
		if isMD(w) {
			out = append(out, w)
		}
	}
	return out
}

// isMD reports whether a word names Markdown: a .md or .markdown path or
// glob, or a brace list with one of them.
func isMD(w string) bool {
	l := strings.ToLower(strings.TrimRight(w, "/"))
	if strings.HasSuffix(l, ".md") || strings.HasSuffix(l, ".markdown") {
		return true
	}
	if i := strings.LastIndex(l, ".{"); i >= 0 && strings.HasSuffix(l, "}") {
		for _, alt := range strings.Split(l[i+2:len(l)-1], ",") {
			if alt == "md" || alt == "markdown" {
				return true
			}
		}
	}
	return false
}

// mentionsMD reports whether a name pattern selects Markdown files.
func mentionsMD(w string) bool {
	l := strings.ToLower(w)
	return isMD(w) || strings.Contains(l, ".md") || strings.Contains(l, ".markdown")
}

var reserved = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true,
	"while": true, "until": true, "!": true, "{": true, "}": true, "time": true,
}

// stripPrefix drops reserved words, variable assignments and wrappers that
// run the rest of the words as a command.
func stripPrefix(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case reserved[w]:
			words = words[1:]
		case isAssignment(w):
			words = words[1:]
		case w == "command" || w == "builtin" || w == "exec" || w == "nohup" || w == "stdbuf" || w == "sudo" || w == "env" || w == "nice" || w == "timeout":
			words = words[1:]
			for len(words) > 0 && (strings.HasPrefix(words[0], "-") || isAssignment(words[0])) {
				opt := words[0]
				words = words[1:]
				if len(words) > 0 && (opt == "-n" || opt == "-u" || opt == "-g" || opt == "-s" || opt == "-k") {
					words = words[1:]
				}
			}
			if w == "timeout" && len(words) > 0 {
				words = words[1:] // the duration
			}
		default:
			return words
		}
	}
	return words
}

func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// exists reports whether a path the command names exists. A name the shell
// would expand ($VAR, globs, ~user) counts as existing: the hook cannot tell.
func (b *bash) exists(p string) bool {
	if strings.ContainsAny(p, "$`*?[") || strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "~/") {
		return true
	}
	_, err := os.Stat(resolve(b.cwd, p))
	return err == nil
}

func (b *bash) isDir(cwd, p string) bool {
	fi, err := os.Stat(resolve(cwd, p))
	return err == nil && fi.IsDir()
}

// maxWalk bounds the entries hasMD looks at; a larger tree counts as
// holding Markdown.
const maxWalk = 20000

// hasMD reports whether a directory tree holds a Markdown file, so a
// recursive search of a tree without one passes. .git is skipped.
func hasMD(dir string) bool {
	n, found := 0, false
	errStop := errors.New("stop")
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable parts: the search cannot read them either
		}
		if n++; n > maxWalk {
			found = true
			return errStop
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && isMD(d.Name()) {
			found = true
			return errStop
		}
		return nil
	})
	return found || err != nil && !errors.Is(err, errStop)
}

// resolve makes p absolute against cwd, expanding a leading ~/.
func resolve(cwd, p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(p) || cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}
