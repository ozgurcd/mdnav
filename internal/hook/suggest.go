package hook

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ozgurcd/mdnav/internal/config"
	"github.com/ozgurcd/mdnav/internal/edit"
	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
)

const escapeHelp = "If mdnav cannot do this, rerun the command with a comment that says why: # mdnav-cannot: <specific reason>"

// message returns the mdnav command for a finding, which works as written,
// and the denial message that names it.
func (f finding) message(cwd string) (cmd, msg string) {
	var b strings.Builder
	target := strings.Join(f.files, " ")
	if target == "" {
		target = strings.Join(f.paths, " ")
	}
	if target == "" {
		target = "the working tree"
	}
	switch f.rule {
	case ruleRead:
		file := firstFile(cwd, f.files)
		switch {
		case file != "" && f.line > 0 && blockAt(resolve(cwd, file), f.line):
			cmd = mdnav("get", nil, fmt.Sprintf("%s:%d", file, f.line))
		case file != "":
			cmd = mdnav("outline", nil, file)
		default:
			cmd = listCommand(f)
		}
		fmt.Fprintf(&b, "mdnav hook: read Markdown with mdnav, not %s (%s).\nRun: %s\n", f.prog, target, cmd)
		fmt.Fprintf(&b, "Then read one block: mdnav get '<id, § number, heading or text>' %s   (or FILE:LINE for the block at a line)\n", orFile(file))
	case ruleSearch:
		cmd = searchCommand(f)
		fmt.Fprintf(&b, "mdnav hook: search Markdown with mdnav, not %s (%s).\nRun: %s\n", f.prog, target, cmd)
		if f.recursive && f.hint != "" {
			fmt.Fprintf(&b, "To search only the other files, keep Markdown out: %s ... %s\n", f.prog, f.hint)
		}
	case ruleWrite:
		file := firstFile(cwd, f.files)
		if f.sedSub && file != "" {
			cmd, _ = subCommands(cwd, file, []pair{{Old: f.sedOld, New: f.sedNew}})
		}
		switch {
		case cmd != "":
		case file != "":
			cmd = mdnav("outline", nil, file)
		default:
			cmd = listCommand(f)
		}
		fmt.Fprintf(&b, "mdnav hook: edit Markdown with mdnav, not %s (%s).\nRun: %s\n", f.prog, target, cmd)
		b.WriteString(editTemplates(file))
		if f.prog == "mv" || f.prog == "cp" {
			b.WriteString("To rename or copy a file without changing it: git mv SRC DST, or cp/mv into a directory.\n")
		}
	}
	b.WriteString(escapeHelp)
	return cmd, b.String()
}

func orFile(file string) string {
	if file == "" {
		return "FILE"
	}
	return word(file)
}

// firstFile returns the first Markdown file that exists, as written.
func firstFile(cwd string, files []string) string {
	for _, f := range files {
		if isFile(resolve(cwd, f)) {
			return f
		}
	}
	return ""
}

// listCommand lists the Markdown files a command reached through a glob,
// find or xargs.
func listCommand(f finding) string {
	paths := f.paths
	if len(paths) == 0 {
		paths = f.files
	}
	return mdnav("get", []string{"--files-only", "--regex"}, append([]string{"."}, paths...)...)
}

// blockAt reports whether line n of a Markdown file starts a block that
// mdnav get path:line can print.
func blockAt(path string, n int) bool {
	raw, err := readLimited(path)
	if err != nil {
		return false
	}
	doc := markdown.Parse(path, raw)
	_, ok := doc.Smallest(n)
	return n <= doc.NumLines() && ok
}

func searchCommand(f finding) string {
	pat, regex, ok := mdnavPattern(f.patterns, f.syntax)
	if !ok {
		return listCommand(f)
	}
	var flags []string
	if regex {
		flags = []string{"--regex"}
	}
	return mdnav("get", flags, append([]string{pat}, f.paths...)...)
}

var breEscapes = strings.NewReplacer(`\|`, "|", `\(`, "(", `\)`, ")", `\{`, "{", `\}`, "}", `\+`, "+", `\?`, "?")

// mdnavPattern turns grep-style patterns into one mdnav get query: plain
// text when that finds the same lines, else a Go regular expression.
func mdnavPattern(pats []string, syntax string) (pat string, regex, ok bool) {
	if len(pats) == 0 {
		return "", false, false
	}
	var res []string
	for _, p := range pats {
		switch syntax {
		case "fixed":
			p = regexp.QuoteMeta(p)
		case "bre":
			p = breEscapes.Replace(p)
		}
		res = append(res, p)
	}
	if len(res) == 1 && !strings.ContainsAny(strings.ReplaceAll(res[0], `\.`, "."), `\[]*^$+?(){}|`) {
		return strings.ReplaceAll(res[0], `\.`, "."), false, true
	}
	pat = res[0]
	if len(res) > 1 {
		pat = "(?:" + strings.Join(res, ")|(?:") + ")"
	}
	if _, err := regexp.Compile(pat); err != nil {
		return "", false, false
	}
	return pat, true, true
}

func editTemplates(file string) string {
	f := orFile(file)
	return "Then change one block (mdnav get prints each block's #hash; <block> is an id, § number, heading, text or FILE:LINE):\n" +
		"  mdnav sub '<block>' '<old text>' '<new text>' " + f + "\n" +
		"  mdnav set '| <row id> |' '<value>' --col '<header>' " + f + "\n" +
		"  mdnav replace '<block>' --from NEW.txt --expect '<hash>' " + f + "\n" +
		"  mdnav insert --after '<block>' --from NEW.txt " + f + "\n" +
		"  mdnav delete '<block>' --expect '<hash>' " + f + "\n" +
		"  mdnav add-row '<row of the table>' --cells 'a|b|c' " + f + "\n" +
		"  mdnav apply EDITS.json   (several edits to one file, all or nothing; --from - reads standard input)\n"
}

// pair is one old/new text replacement. Path is set when a multi-file
// edit names each edit's file.
type pair struct {
	Path     string
	Old, New string
	All      bool
}

// subCommands returns mdnav sub commands that make the replacements in
// file (as written, relative to cwd) in order, after checking with a dry run
// that they apply; ok is false when they do not.
func subCommands(cwd, file string, pairs []pair) (cmd string, ok bool) {
	abs := resolve(cwd, file)
	raw, err := readLimited(abs)
	if err != nil {
		return "", false
	}
	content := string(raw)
	var cmds []string
	var ops []edit.Op
	size := 0
	for _, p := range pairs {
		if p.All || p.Old == "" || strings.Count(content, p.Old) != 1 || strings.Contains(p.Old, "\n\n") {
			return "", false
		}
		i := strings.Index(content, p.Old)
		line := 1 + strings.Count(content[:i], "\n")
		old := p.Old
		ops = append(ops, edit.Op{Op: edit.OpSub, Target: abs + ":" + strconv.Itoa(line), Old: &old, New: p.New})
		cmds = append(cmds, mdnav("sub", nil, file+":"+strconv.Itoa(line), p.Old, p.New))
		content = strings.Replace(content, p.Old, p.New, 1)
		if size += len(p.Old) + len(p.New); size > 4000 {
			return "", false
		}
	}
	ids, err := idpat.New(config.Default().Patterns)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := edit.Apply(ctx, ids, abs, ops, true); err != nil {
		return "", false
	}
	return strings.Join(cmds, " && "), true
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const limit = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && len(raw) > limit {
		err = fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return raw, err
}

// mdnav builds a mdnav command line: flags first, then "--" when an argument
// is shaped like a flag, then the arguments, each one shell word.
func mdnav(sub string, flags []string, args ...string) string {
	parts := append([]string{"mdnav", sub}, flags...)
	for _, a := range args {
		if flagShaped.MatchString(a) {
			parts = append(parts, "--")
			break
		}
	}
	for _, a := range args {
		parts = append(parts, word(a))
	}
	return strings.Join(parts, " ")
}

var (
	flagShaped = regexp.MustCompile(`^--?[A-Za-z]`)
	plainWord  = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,~-]+$`)
	globWord   = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,~*?{}-]+$`)
)

// word returns s as one shell word, single-quoted unless it is plain.
// Globs stay unquoted so the shell expands them as it did for the original
// command.
func word(s string) string {
	if plainWord.MatchString(s) || globWord.MatchString(s) && strings.ContainsAny(s, "*?") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
