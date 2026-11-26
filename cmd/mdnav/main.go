// Command mdnav prints the smallest complete Markdown block that answers a
// query (an identifier, a § number, a heading or free text), always with its
// file and line range.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ozgurcd/mdnav/internal/app"
	"github.com/ozgurcd/mdnav/internal/buildinfo"
	"github.com/ozgurcd/mdnav/internal/edit"
	"github.com/ozgurcd/mdnav/internal/markdown"
	"github.com/ozgurcd/mdnav/internal/nav"
)

const usage = `mdnav: navigate Markdown by identifier, § number, heading or text.

Usage:
  mdnav get <query> [paths...] [--limit N] [--max-lines N] [--regex] [--json] [--files-only]
  mdnav outline <file> [--depth N] [--max-lines N] [--json]
  mdnav refs <id> [paths...] [--include-def] [--full] [--limit N] [--json]
  mdnav status <id> [paths...] [--full] [--limit N] [--json]
  mdnav ids [paths...] [--pattern NAME] [--limit N] [--json]
  mdnav version [--json]          (also: mdnav --version)

Edit (one block, addressed like get or as path:line; writes unless --dry-run):
  mdnav sub <target> <old> <new> [paths...] [--expect H]       replace the one occurrence of <old>
  mdnav set <target> <value> --col HEADER [--old V] [--expect H] [paths...]   set a table cell
  mdnav replace <target> --from FILE|- (--expect H | --force) [paths...]       replace the block
  mdnav insert (--before|--after) <target> --from FILE|- [--expect H] [paths...]
  mdnav delete <target> (--expect H | --force) [paths...]
  mdnav add-row <target> --cells 'a|b|c' [--expect H] [paths...]  append a row to the table
  mdnav apply EDITS.json|-                                       several edits to one file, all or nothing
  Every edit takes --dry-run and --json. H is the #hash get prints for the block.

Paths default to the current directory, searched recursively for *.md files,
skipping .git and git-ignored paths. Flags may come before or after arguments.
"--" ends the flags: every argument after it is an argument, so text that
looks like a flag goes there:  mdnav sub -- D4 '-x' '-y' docs
An argument that starts with "-" but is not shaped like a flag ("->", "-1",
"- item") needs no "--".

get resolves the query in order: exact id (defining table row or heading),
§ number ("§5.4" or "5.4"), heading text, then free text. --regex skips the
first three steps.

Common flags:
  --config FILE   identifier patterns (default: .mdnav.json in . or a parent)
  --timeout D     give up after this long (default 30s)

Exit status: 0 on success, 1 when nothing matched, 2 on usage errors or failure,
3 when an edit's --expect or --old no longer matches the file.
`

// version is the source version; release builds override it with -ldflags
// "-X main.version=vX.Y.Z".
var version = buildinfo.Version

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	req, timeout, err := parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "mdnav: %v\n\n%s", err, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req.Stdin = os.Stdin
	switch err := app.Run(ctx, req, stdout); {
	case err == nil:
		return 0
	case errors.Is(err, app.ErrNoMatch):
		return 1
	case errors.Is(err, nav.ErrNoTarget):
		fmt.Fprintf(stderr, "mdnav: %v\n", err)
		return 1
	case errors.Is(err, edit.ErrStale):
		fmt.Fprintf(stderr, "mdnav: %v\n", err)
		return 3
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(stderr, "mdnav: timed out after %s\n", timeout)
		return 2
	default:
		fmt.Fprintf(stderr, "mdnav: %v\n", err)
		return 2
	}
}

// parse turns the command line into a request.
func parse(args []string) (app.Request, time.Duration, error) {
	var req app.Request
	if len(args) == 0 {
		return req, 0, errors.New("missing command")
	}
	req.Command = args[0]
	req.Version = version
	switch req.Command {
	case "help", "-h", "--help":
		return req, 0, flag.ErrHelp
	case "--version":
		req.Command = app.CmdVersion
	}

	fs := flag.NewFlagSet("mdnav "+req.Command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := fs.Duration("timeout", 30*time.Second, "")
	fs.StringVar(&req.ConfigPath, "config", "", "")
	fs.BoolVar(&req.JSON, "json", false, "")
	o := &req.Options

	var wantArg bool
	var ef *editOpts
	switch req.Command {
	case app.CmdGet:
		wantArg = true
		fs.IntVar(&o.Limit, "limit", nav.DefaultGetLimit, "")
		fs.IntVar(&o.MaxLines, "max-lines", nav.DefaultMaxLines, "")
		fs.BoolVar(&o.Regex, "regex", false, "")
		fs.BoolVar(&req.FilesOnly, "files-only", false, "")
	case app.CmdOutline:
		wantArg = true
		fs.IntVar(&o.Depth, "depth", 0, "")
		fs.IntVar(&o.MaxLines, "max-lines", nav.DefaultMaxLines, "")
	case app.CmdRefs:
		wantArg = true
		fs.BoolVar(&o.IncludeDef, "include-def", false, "")
		fs.BoolVar(&o.Full, "full", false, "")
		fs.IntVar(&o.Limit, "limit", nav.DefaultListLimit, "")
	case app.CmdStatus:
		wantArg = true
		fs.BoolVar(&o.Full, "full", false, "")
		fs.IntVar(&o.Limit, "limit", nav.DefaultListLimit, "")
	case app.CmdIDs:
		fs.StringVar(&o.Pattern, "pattern", "", "")
		fs.IntVar(&o.Limit, "limit", nav.DefaultListLimit, "")
	case app.CmdVersion:
	case app.CmdSub, app.CmdSet, app.CmdReplace, app.CmdInsert, app.CmdDelete, app.CmdAddRow, app.CmdApply:
		ef = editFlags(fs, &req)
	default:
		return req, 0, fmt.Errorf("unknown command %q", req.Command)
	}

	pos, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return req, 0, err
	}
	if ef != nil {
		if pos, err = finishEdit(fs, &req, ef, pos); err != nil {
			return req, 0, err
		}
	}
	if wantArg {
		if len(pos) == 0 {
			return req, 0, fmt.Errorf("%s: missing argument", req.Command)
		}
		req.Arg, pos = pos[0], pos[1:]
	}
	if req.Command == app.CmdVersion && len(pos) > 0 {
		return req, 0, fmt.Errorf("version takes no arguments, got %q", pos)
	}
	if req.Command == app.CmdOutline && len(pos) > 0 {
		return req, 0, fmt.Errorf("outline takes one file, got extra %q", pos)
	}
	req.Paths = pos
	if *timeout <= 0 {
		return req, 0, errors.New("--timeout must be positive")
	}
	if o.MaxLines < 0 {
		return req, 0, errors.New("--max-lines must not be negative")
	}
	if o.Limit == 0 {
		o.Limit = -1 // --limit 0 means no limit
	}
	if req.FilesOnly && !isSet(fs, "limit") {
		o.Limit = -1
	}
	return req, *timeout, nil
}

// parseInterspersed parses flags anywhere among the arguments and returns the
// positional ones in order. "--" ends the flags: every argument after it is
// positional. An argument that starts with "-" but is not shaped like a flag
// ("-", "->", "-1", "- item") is positional too, so old and new text such as
// "-> done" needs no "--".
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(pos, args[i+1:]...), nil
		}
		m := flagShape.FindStringSubmatch(a)
		if m == nil {
			pos = append(pos, a)
			continue
		}
		n := 1 // the flag, plus its value when it takes one and has no "="
		if f := fs.Lookup(m[1]); f != nil && !strings.Contains(a, "=") {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
				n = 2
			}
		}
		end := min(i+n, len(args))
		if err := fs.Parse(args[i:end]); err != nil {
			if fs.Lookup(m[1]) == nil && m[1] != "h" && m[1] != "help" {
				return nil, fmt.Errorf("%w (an argument that starts with - goes after --)", err)
			}
			return nil, err
		}
		i = end - 1
	}
	return pos, nil
}

// flagShape matches -name, --name and --name=value, capturing the name.
var flagShape = regexp.MustCompile(`^--?([A-Za-z][A-Za-z0-9_-]*)(?:=.*)?$`)

func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// editOpts holds edit flags that need checking after parsing.
type editOpts struct {
	old, before, after, cells string
}

// editFlags registers the flags of an edit command.
func editFlags(fs *flag.FlagSet, req *app.Request) *editOpts {
	ef := &editOpts{}
	fs.BoolVar(&req.DryRun, "dry-run", false, "")
	if req.Command == app.CmdApply {
		return ef
	}
	fs.StringVar(&req.Edit.Expect, "expect", "", "")
	switch req.Command {
	case app.CmdSet:
		fs.StringVar(&req.Edit.Column, "col", "", "")
		fs.StringVar(&ef.old, "old", "", "")
	case app.CmdReplace:
		fs.StringVar(&req.From, "from", "", "")
		fs.BoolVar(&req.Edit.Force, "force", false, "")
	case app.CmdInsert:
		fs.StringVar(&ef.before, "before", "", "")
		fs.StringVar(&ef.after, "after", "", "")
		fs.StringVar(&req.From, "from", "", "")
	case app.CmdDelete:
		fs.BoolVar(&req.Edit.Force, "force", false, "")
	case app.CmdAddRow:
		fs.StringVar(&ef.cells, "cells", "", "")
	}
	return ef
}

// finishEdit fills req.Edit from the positional arguments and checked flags,
// and returns the remaining arguments (the paths).
func finishEdit(fs *flag.FlagSet, req *app.Request, ef *editOpts, pos []string) ([]string, error) {
	need := func(n int, what string) error {
		if len(pos) < n {
			return fmt.Errorf("%s: missing %s", req.Command, what)
		}
		return nil
	}
	op := &req.Edit
	switch req.Command {
	case app.CmdSub:
		if err := need(3, "<target> <old> <new>"); err != nil {
			return nil, err
		}
		old := pos[1]
		op.Op, op.Target, op.Old, op.New, pos = edit.OpSub, pos[0], &old, pos[2], pos[3:]
	case app.CmdSet:
		if err := need(2, "<target> <value>"); err != nil {
			return nil, err
		}
		if op.Column == "" {
			return nil, errors.New("set: --col <header> is required")
		}
		if isSet(fs, "old") {
			old := ef.old
			op.Old = &old
		}
		op.Op, op.Target, op.Value, pos = edit.OpSet, pos[0], pos[1], pos[2:]
	case app.CmdReplace, app.CmdDelete, app.CmdAddRow:
		if err := need(1, "<target>"); err != nil {
			return nil, err
		}
		op.Target, pos = pos[0], pos[1:]
		switch req.Command {
		case app.CmdReplace:
			op.Op = edit.OpReplace
			if req.From == "" {
				return nil, errors.New("replace: --from FILE (or - for standard input) is required")
			}
		case app.CmdDelete:
			op.Op = edit.OpDelete
		case app.CmdAddRow:
			op.Op = edit.OpAddRow
			if !isSet(fs, "cells") {
				return nil, errors.New("add-row: --cells 'a|b|c' is required")
			}
			op.Cells = markdown.SplitCells(ef.cells)
		}
	case app.CmdInsert:
		switch {
		case (ef.before == "") == (ef.after == ""):
			return nil, errors.New("insert: give exactly one of --before <target> or --after <target>")
		case ef.before != "":
			op.Op, op.Target = edit.OpInsertBefore, ef.before
		default:
			op.Op, op.Target = edit.OpInsertAfter, ef.after
		}
		if req.From == "" {
			return nil, errors.New("insert: --from FILE (or - for standard input) is required")
		}
	case app.CmdApply:
		if len(pos) != 1 {
			return nil, errors.New("apply: give exactly one edits file (or - for standard input)")
		}
		req.Arg, pos = pos[0], nil
	}
	if req.Edit.Force && req.Edit.Expect != "" {
		return nil, errors.New("--force and --expect exclude each other")
	}
	return pos, nil
}
