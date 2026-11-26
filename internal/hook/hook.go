// Package hook is mdnav's Claude Code PreToolUse hook: it denies Bash
// commands that read, search or write Markdown with general tools, and Edit,
// MultiEdit or Write calls that change an existing Markdown file, and names
// the mdnav command that does the job. It judges only .md and .markdown paths.
package hook

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ozgurcd/mdnav/internal/shell"
)

// Input is the part of the Claude Code PreToolUse payload the hook reads.
type Input struct {
	ToolName  string         `json:"tool_name"`
	ToolInput jsontext.Value `json:"tool_input"`
	Cwd       string         `json:"cwd"`
}

// Verdicts.
const (
	Allow  = "allow"
	Deny   = "deny"
	Warn   = "warn"   // would deny; allowed in warn mode and logged
	Escape = "escape" // allowed by a "# mdnav-cannot: <reason>" comment and logged
)

// Outcome is the hook's answer to one tool call.
type Outcome struct {
	Verdict string
	Reason  string // the denial message, also kept for warn
	Command string // the mdnav command the denial names
	Log     string // the line to append to MDNAV_CANNOT_LOG, "" for none
}

// decision is what the rules say before the mode and the escape apply.
type decision struct {
	rule    string // "" when the call is allowed
	reason  string
	command string
}

// Evaluate judges one tool call. warn turns a denial into an allowed,
// logged call.
func Evaluate(in Input, warn bool, now time.Time) Outcome {
	var d decision
	what := in.ToolName
	escape, vague := "", false
	switch in.ToolName {
	case "Bash":
		var bi struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(in.ToolInput, &bi) != nil {
			return Outcome{Verdict: Allow}
		}
		what = bi.Command
		sc, err := shell.Parse(bi.Command)
		if err != nil {
			return Outcome{Verdict: Allow} // bash rejects it too
		}
		escape, vague = escapeReason(sc.Comments)
		b := &bash{root: in.Cwd, cwd: in.Cwd}
		b.script(sc, 0)
		d = b.decide()
	case "Edit", "MultiEdit", "Write":
		d, what = decideTool(in)
	default:
		return Outcome{Verdict: Allow}
	}

	switch {
	case escape != "":
		return Outcome{Verdict: Escape, Command: d.command, Log: logLine(now, in.Cwd, "cannot: "+escape, what)}
	case d.rule == "":
		return Outcome{Verdict: Allow}
	case vague:
		d.reason += "\nThe mdnav-cannot reason is too vague: name what mdnav cannot do (three words or more)."
	}
	if warn {
		return Outcome{Verdict: Warn, Reason: d.reason, Command: d.command,
			Log: logLine(now, in.Cwd, "warn "+d.rule+": "+d.command, what)}
	}
	return Outcome{Verdict: Deny, Reason: d.reason, Command: d.command}
}

// escapeReason returns the reason of a "# mdnav-cannot: <reason>" comment,
// or reports that the reason is too vague to count.
func escapeReason(comments []string) (reason string, vague bool) {
	for _, c := range comments {
		r, ok := strings.CutPrefix(strings.TrimSpace(c), "mdnav-cannot:")
		if !ok {
			continue
		}
		r = strings.TrimSpace(r)
		if len(strings.Fields(r)) < 3 {
			vague = true
			continue
		}
		return r, false
	}
	return "", vague
}

// decide turns the findings into one decision: the one with the highest
// rule, the first of those.
func (b *bash) decide() decision {
	best := -1
	for i, f := range b.found {
		if best < 0 || rank(f.rule) > rank(b.found[best].rule) {
			best = i
		}
	}
	if best < 0 {
		return decision{}
	}
	f := b.found[best]
	cmd, msg := f.message(b.root)
	return decision{rule: f.rule, reason: msg, command: cmd}
}

// logLine is one tab-separated MDNAV_CANNOT_LOG line: time, cwd, reason and
// the command, quoted so it stays on one line.
func logLine(now time.Time, cwd, reason, command string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r < ' ' || r == 0x7f {
				return ' '
			}
			return r
		}, s)
	}
	return strings.Join([]string{now.UTC().Format(time.RFC3339), clean(cwd), clean(reason), strconv.Quote(command)}, "\t") + "\n"
}

// display returns the path a message shows for abs: relative to cwd when
// it is inside it.
func display(cwd, abs string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel
		}
	}
	return abs
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
