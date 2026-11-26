package hook

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"time"
)

// Usage is the help of mdnav hook.
const Usage = `mdnav hook: Claude Code PreToolUse hook that makes agents use mdnav for Markdown.

Usage:
  mdnav hook              read the PreToolUse JSON on standard input and answer it
  mdnav hook --selftest   run the hook's decision table and exit 0 when every row passes

It denies Bash commands that read (cat, head, tail, less, more, nl, sed, awk),
search (grep, rg, ag, git grep) or write (sed -i, perl -i, tee, >, >>, >|, &>,
cp, mv, install, dd of=, truncate) an existing .md or .markdown file, and Edit,
MultiEdit or Write calls that change an existing one. Each denial names the
mdnav command to run instead. Counts, checksums, listings, git diff, git show
and git log, and mdnav itself pass. A command carrying the comment
"# mdnav-cannot: <specific reason>" passes.

Environment:
  MDNAV_HOOK_MODE   warn (default): allow and log what would be denied; deny: deny
  MDNAV_CANNOT_LOG  file to append one line per mdnav-cannot use and per warn-mode
                    denial: time, cwd, reason, command (tab-separated); unset: no log

Register it for the matchers Bash and Edit|Write|MultiEdit.
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
// 0: allowed with no output, denied with the hook output JSON. Input it
// cannot read exits 1, which Claude Code reports without blocking the call.
func Main(args []string, env Env) int {
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help"):
		fmt.Fprint(env.Stdout, Usage)
		return 0
	case len(args) == 1 && args[0] == "--selftest":
		if err := SelfTest(env.Stdout, env.Run); err != nil {
			fmt.Fprintf(env.Stderr, "mdnav: %v\n", err)
			return 1
		}
		return 0
	case len(args) > 0:
		fmt.Fprintf(env.Stderr, "mdnav: hook takes no arguments but --selftest, got %q\n\n%s", args, Usage)
		return 2
	}

	raw, err := io.ReadAll(io.LimitReader(env.Stdin, maxInput+1))
	if err == nil && len(raw) > maxInput {
		err = fmt.Errorf("input larger than %d bytes", maxInput)
	}
	var in Input
	if err == nil {
		err = json.Unmarshal(raw, &in)
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
	out := Evaluate(in, warn, env.Now())
	if out.Log != "" {
		if path := env.Getenv("MDNAV_CANNOT_LOG"); path != "" {
			if err := appendLine(path, out.Log); err != nil {
				fmt.Fprintf(env.Stderr, "mdnav hook: %v\n", err)
			}
		}
	}
	if out.Verdict != Deny {
		return 0
	}
	resp := map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": out.Reason,
	}}
	if err := json.MarshalWrite(env.Stdout, resp, json.Deterministic(true)); err != nil {
		fmt.Fprintf(env.Stderr, "mdnav hook: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout)
	return 0
}

// appendLine appends one line with a single write: O_APPEND keeps lines
// from hooks that run at the same time whole.
func appendLine(path, line string) error {
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
