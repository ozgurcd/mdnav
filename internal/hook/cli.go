package hook

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Usage is the help of mdnav hook.
const Usage = `mdnav hook: PreToolUse hook that makes coding agents use mdnav for Markdown.

Usage:
  mdnav hook [--agent AGENT]               read one tool call on standard input and answer it
  mdnav hook [--agent AGENT] --record DIR  write the raw input to DIR/<agent>-<time>.json and allow it
  mdnav hook --selftest [--fixtures DIR]   run the decision table for every agent, then replay
                                           the recorded fixtures; exit 0 when all pass
  mdnav hook --write-fixture DIR           write the tree fixtures are recorded in

AGENT is claude (default), codex, opencode or agy. An agent whose format has
no recorded fixtures yet is refused: exit 2, "no fixture-backed format for
<agent>". Fixture-backed now: claude, codex, opencode.

It denies shell commands that read (cat, head, tail, less, more, nl, sed, awk),
search (grep, rg, ag, git grep) or write (sed -i, perl -i, tee, >, >>, >|, &>,
cp, mv, install, dd of=, truncate) an existing .md or .markdown file, and
edit, write or apply_patch calls that change an existing one. Each denial
names the mdnav command to run instead. Counts, checksums, listings, git
diff, git show and git log, and mdnav itself pass. A command carrying the
comment "# mdnav-cannot: <specific reason>" passes. A shell or write call
whose input cannot be read is denied, naming the tool; an unknown read-only
tool passes.

Environment:
  MDNAV_HOOK_MODE   warn (default): allow and log what would be denied; deny: deny
  MDNAV_CANNOT_LOG  file to append one line per mdnav-cannot use and per warn-mode
                    denial: time, cwd, reason, command, agent (tab-separated);
                    unset: no log

Recording (one session per agent; the recordings become fixtures):
  mdnav hook --write-fixture /tmp/mdnav-fx, start the agent in /tmp/mdnav-fx with
  the hook command "mdnav hook --agent AGENT --record DIR", and have it read,
  search, edit, write, patch and create README.md and docs/guide.md. Files are
  0600 in a 0700 DIR; file content is cut to 4 KiB, environment values are
  dropped and token-like strings redacted; commands and patches are kept.
  Move each recording to <fixtures>/<agent>/ and add <name>.want holding
  "<verdict> <tool_kind>", such as "deny edit". Fixture folder default:
  ` + DefaultFixtures + `

Install pack (the mdnav guard only):
  mdnav hook --print-install --agent AGENT          print AGENT's complete registration
  mdnav hook --check-install --agent AGENT [--json] read only AGENT's hook registration and
      report the mdnav guard per tool group: registered | missing | different from
      --print-install. Exit 0 all registered, 1 missing or different, 2 cannot read.
  nuntius lanes start each agent with the owner's config, so a registration
  checked here covers that agent's lanes too.

Lane proof (after install): one short lane per agent runs "cat <a .md file>"
and one text search of a .md file. Each must be denied (deny mode) or appear
in that agent's MDNAV_CANNOT_LOG (warn mode), the line ending in the agent.

The registration --print-install gives each agent follows.
`

// Env is what Main needs from the process.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Now            func() time.Time
	Run            Runner // mdnav itself, for --selftest
}

const maxInput = 4 << 20

// Main runs mdnav hook and returns the exit status. An answered call exits
// 0: allowed with no output, denied with the agent's denial output. Input
// it cannot read exits 1, which Claude Code reports without blocking the
// call. An unknown agent, or one without fixtures, exits 2.
func Main(args []string, env Env) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "mdnav hook: %v\n\n%s", err, Usage)
		return 2
	}
	switch {
	case o.help:
		fmt.Fprint(env.Stdout, Usage)
		for _, agent := range Agents {
			fmt.Fprintln(env.Stdout)
			PrintInstall(env.Stdout, agent, env.Getenv)
		}
		return 0
	case o.printInstall:
		return PrintInstall(env.Stdout, o.agent, env.Getenv)
	case o.checkInstall:
		return CheckInstall(env.Stdout, o.agent, o.json, env.Getenv)
	case o.selftest:
		if err := SelfTest(env.Stdout, env.Run, o.fixtures); err != nil {
			fmt.Fprintf(env.Stderr, "mdnav: %v\n", err)
			return 1
		}
		return 0
	case o.writeFixture != "":
		if err := WriteFixture(o.writeFixture); err != nil {
			fmt.Fprintf(env.Stderr, "mdnav hook: %v\n", err)
			return 1
		}
		return 0
	}

	raw, err := io.ReadAll(io.LimitReader(env.Stdin, maxInput+1))
	if err == nil && len(raw) > maxInput {
		err = fmt.Errorf("input larger than %d bytes", maxInput)
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "mdnav hook: cannot read the hook input: %v\n", err)
		return 1
	}
	if o.record != "" {
		if _, err := Record(o.record, o.agent, raw, env.Now()); err != nil {
			fmt.Fprintf(env.Stderr, "mdnav hook: record: %v\n", err)
			return 1
		}
		return 0
	}
	ev, err := Read(o.agent, raw)
	if errors.Is(err, ErrNoFixtures) {
		fmt.Fprintf(env.Stderr, "mdnav hook: %v; record its calls with --record DIR first\n", err)
		return 2
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "mdnav hook: cannot read the hook input: %v\n", err)
		return 1
	}

	warn := true
	switch m := env.Getenv("MDNAV_HOOK_MODE"); m {
	case "", "warn":
	case "deny":
		warn = false
	default:
		fmt.Fprintf(env.Stderr, "mdnav hook: MDNAV_HOOK_MODE=%q is not warn or deny; using warn\n", m)
	}
	out := Evaluate(ev, warn, env.Now())
	if out.Log != "" {
		if path := env.Getenv("MDNAV_CANNOT_LOG"); path != "" {
			if err := appendLine(path, out.Log); err != nil {
				fmt.Fprintf(env.Stderr, "mdnav hook: %v\n", err)
			}
		}
	}
	code, err := Write(o.agent, out, env.Stdout)
	if err != nil {
		fmt.Fprintf(env.Stderr, "mdnav hook: %v\n", err)
	}
	return code
}

type options struct {
	agent, record, fixtures, writeFixture string
	help, selftest, printInstall          bool
	checkInstall, json                    bool
}

// parseArgs reads the flags of mdnav hook, as --flag value or --flag=value.
func parseArgs(args []string) (options, error) {
	o := options{agent: AgentClaude, fixtures: DefaultFixtures}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) || args[i+1] == "" {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "-h", "--help", "help":
			o.help = true
		case "--selftest":
			o.selftest = true
		case "--print-install":
			o.printInstall = true
		case "--check-install":
			o.checkInstall = true
		case "--json":
			o.json = true
		case "--agent":
			o.agent, err = value()
		case "--record":
			o.record, err = value()
		case "--fixtures":
			o.fixtures, err = value()
		case "--write-fixture":
			o.writeFixture, err = value()
		default:
			err = fmt.Errorf("unknown argument %q", args[i])
		}
		if err != nil {
			return o, err
		}
	}
	if o.json && !o.checkInstall {
		return o, errors.New("--json goes with --check-install")
	}
	if !slices.Contains(Agents, o.agent) {
		return o, fmt.Errorf("unknown agent %q: use one of %s", o.agent, strings.Join(Agents, ", "))
	}
	return o, nil
}

// appendLine appends one line with a single write: O_APPEND keeps lines
// from hooks that run at the same time whole.
// A missing directory is made, 0700.
func appendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(line)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
