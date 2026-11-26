// Package hook is mdnav's PreToolUse hook for coding agents: it denies
// shell commands that read, search or write Markdown with general tools, and
// edit, write or patch calls that change an existing Markdown file, and
// names the mdnav command that does the job. It judges only .md and
// .markdown paths. A read layer per agent turns the agent's input into an
// Event, the rules judge the Event, and a write layer per agent answers.
package hook

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ozgurcd/mdnav/internal/shell"
)

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
// logged call. A shell or write call the read layer could not read is
// denied in either mode.
func Evaluate(ev Event, warn bool, now time.Time) Outcome {
	var d decision
	what := ev.Tool
	escape, vague := "", false
	if ev.Unread != "" && ev.Kind != KindRead && ev.Kind != KindOther {
		return Outcome{Verdict: Deny, Reason: "mdnav hook: cannot read the " + ev.Agent + " " + ev.Tool +
			" call (" + ev.Unread + "); denied so Markdown rules are not skipped. Report this to the mdnav hook owner."}
	}
	switch ev.Kind {
	case KindShell:
		what = ev.Command
		sc, err := shell.Parse(ev.Command)
		if err != nil {
			return Outcome{Verdict: Allow} // bash rejects it too
		}
		escape, vague = escapeReason(sc.Comments)
		b := &bash{root: ev.Cwd, cwd: ev.Cwd}
		b.script(sc, 0)
		d = b.decide()
	case KindEdit, KindMultiEdit, KindWrite:
		d, what = decideTool(ev)
	case KindPatch:
		d, what = decidePatch(ev)
	default:
		return Outcome{Verdict: Allow}
	}

	switch {
	case escape != "":
		return Outcome{Verdict: Escape, Command: d.command, Log: logLine(now, ev.Agent, ev.Cwd, "cannot: "+escape, what)}
	case d.rule == "":
		return Outcome{Verdict: Allow}
	case vague:
		d.reason += "\nThe mdnav-cannot reason is too vague: name what mdnav cannot do (three words or more)."
	}
	if warn {
		return Outcome{Verdict: Warn, Reason: d.reason, Command: d.command,
			Log: logLine(now, ev.Agent, ev.Cwd, "warn "+d.rule+": "+d.command, what)}
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

// logLine is one tab-separated MDNAV_CANNOT_LOG line: time, cwd, reason,
// the command, quoted so it stays on one line, and the agent. The agent is
// last so readers of the earlier four columns keep working.
func logLine(now time.Time, agent, cwd, reason, command string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r < ' ' || r == 0x7f {
				return ' '
			}
			return r
		}, s)
	}
	return strings.Join([]string{now.UTC().Format(time.RFC3339), clean(cwd), clean(reason), strconv.Quote(command), clean(agent)}, "\t") + "\n"
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
