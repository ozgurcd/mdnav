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
	"syscall"
	"time"

	"github.com/ozgurcd/mdnav/internal/app"
	"github.com/ozgurcd/mdnav/internal/nav"
)

const usage = `mdnav: navigate Markdown by identifier, § number, heading or text.

Usage:
  mdnav get <query> [paths...] [--limit N] [--max-lines N] [--regex] [--json] [--files-only]
  mdnav outline <file> [--depth N] [--max-lines N] [--json]
  mdnav refs <id> [paths...] [--include-def] [--full] [--limit N] [--json]
  mdnav status <id> [paths...] [--full] [--limit N] [--json]
  mdnav ids [paths...] [--pattern NAME] [--limit N] [--json]

Paths default to the current directory, searched recursively for *.md files,
skipping .git and git-ignored paths. Flags may come before or after arguments.

get resolves the query in order: exact id (defining table row or heading),
§ number ("§5.4" or "5.4"), heading text, then free text. --regex skips the
first three steps.

Common flags:
  --config FILE   identifier patterns (default: .mdnav.json in . or a parent)
  --timeout D     give up after this long (default 30s)

Exit status: 0 on success, 1 when nothing matched, 2 on usage errors or failure.
`

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

	switch err := app.Run(ctx, req, stdout); {
	case err == nil:
		return 0
	case errors.Is(err, app.ErrNoMatch):
		return 1
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
	if req.Command == "help" || req.Command == "-h" || req.Command == "--help" {
		return req, 0, flag.ErrHelp
	}

	fs := flag.NewFlagSet("mdnav "+req.Command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := fs.Duration("timeout", 30*time.Second, "")
	fs.StringVar(&req.ConfigPath, "config", "", "")
	fs.BoolVar(&req.JSON, "json", false, "")
	o := &req.Options

	var wantArg bool
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
	default:
		return req, 0, fmt.Errorf("unknown command %q", req.Command)
	}

	pos, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return req, 0, err
	}
	if wantArg {
		if len(pos) == 0 {
			return req, 0, fmt.Errorf("%s: missing argument", req.Command)
		}
		req.Arg, pos = pos[0], pos[1:]
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

// parseInterspersed parses flags that may appear before, between or after
// positional arguments. "--" ends flag parsing.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		// flag stops at "--" (consumed) or at the first non-flag argument.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
