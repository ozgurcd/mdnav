package hook

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozgurcd/mdnav/internal/shell"
)

// Case is one row of the hook's decision table. The unit test and
// mdnav hook --selftest run the same rows.
//
// A row runs once for each agent in Agents, or for every agent with a read
// layer when Agents is empty; Input renders it in that agent's format.
type Case struct {
	Name    string
	Agents  []string
	Tool    string // Bash, Edit, MultiEdit, Write or Patch, as Claude names them
	Command string // Bash
	File    string // Edit, MultiEdit, Write
	Edits   []pair // Edit (one) or MultiEdit
	Patch   string // Patch: the apply_patch text, paths relative to the case dir
	Bad     bool   // send a tool input the read layer cannot read
	Warn    bool   // run in warn mode; every other row runs in deny mode
	Want    string // Allow, Deny, Warn or Escape
}

// readable lists the agents with a read layer, fixture-backed or not.
var readable = []string{AgentClaude, AgentCodex, AgentOpencode, AgentAgy}

// Runs returns the agents a case runs for.
func (c Case) Runs() []string {
	if len(c.Agents) > 0 {
		return c.Agents
	}
	return readable
}

// Fixture is the working directory every case runs in.
var Fixture = map[string]string{
	"docs/guide.md":   "# Guide\n\n## Setup\n\nInstall the tool with make install.\nRun it -> done.\n\n| Name | Value |\n| --- | --- |\n| Alpha | one |\n",
	"README.md":       "# Readme\n\nSee the guide.\n",
	"main.go":         "package main\n",
	"notes.txt":       "install notes\n",
	"internal/app.go": "package app // install\n",
}

// Cases is the decision table: one row per deny shape and per allow shape.
var Cases = []Case{
	// Reads.
	{Name: "read cat", Tool: "Bash", Command: "cat docs/guide.md", Want: Deny},
	{Name: "read sed -n lines", Tool: "Bash", Command: "sed -n '5,6p' docs/guide.md", Want: Deny},
	{Name: "read head", Tool: "Bash", Command: "head -n 20 README.md", Want: Deny},
	{Name: "read tail in $()", Tool: "Bash", Command: `echo "$(tail -3 README.md)"`, Want: Deny},
	{Name: "read awk", Tool: "Bash", Command: "awk 'NR<5' docs/guide.md", Want: Deny},
	{Name: "read less in bash -c", Tool: "Bash", Command: "bash -c 'less docs/guide.md'", Want: Deny},
	{Name: "read nl after cd", Tool: "Bash", Command: "cd docs && nl guide.md", Want: Deny},
	{Name: "read input redirect loop", Tool: "Bash", Command: "while read -r l; do echo \"$l\"; done < README.md", Want: Deny},
	// Searches.
	{Name: "search grep file", Tool: "Bash", Command: "grep -n 'make install' docs/guide.md", Want: Deny},
	{Name: "search grep -r dir", Tool: "Bash", Command: "grep -rn install docs", Want: Deny},
	{Name: "search grep --include md", Tool: "Bash", Command: "grep -rn --include='*.md' 'Install\\|Run' .", Want: Deny},
	{Name: "search rg -g md", Tool: "Bash", Command: "rg -g '*.md' install", Want: Deny},
	{Name: "search rg -t md regex", Tool: "Bash", Command: "rg -t md 'Inst+all'", Want: Deny},
	{Name: "search rg whole tree", Tool: "Bash", Command: "rg -n install", Want: Deny},
	{Name: "search ag dir", Tool: "Bash", Command: "ag install docs", Want: Deny},
	{Name: "search git grep", Tool: "Bash", Command: "git grep -n install", Want: Deny},
	{Name: "search git grep md pathspec", Tool: "Bash", Command: "git grep install -- '*.md'", Want: Deny},
	{Name: "search find -exec grep", Tool: "Bash", Command: "find docs -name '*.md' -exec grep -l install {} +", Want: Deny},
	{Name: "search xargs grep", Tool: "Bash", Command: "git ls-files '*.md' | xargs grep install", Want: Deny},
	{Name: "search dash pattern", Tool: "Bash", Command: "grep -n -e '-> done' docs/guide.md", Want: Deny},
	// Writes.
	{Name: "write sed -i literal", Tool: "Bash", Command: "sed -i 's/make install/make setup/' docs/guide.md", Want: Deny},
	{Name: "write sed -i regex", Tool: "Bash", Command: "sed -i -E 's/[0-9]+/N/g' docs/guide.md", Want: Deny},
	{Name: "write perl -i", Tool: "Bash", Command: "perl -pi -e 's/one/two/' docs/guide.md", Want: Deny},
	{Name: "write >", Tool: "Bash", Command: "echo hi > README.md", Want: Deny},
	{Name: "write >>", Tool: "Bash", Command: "printf 'x\\n' >> README.md", Want: Deny},
	{Name: "write >|", Tool: "Bash", Command: "date >| README.md", Want: Deny},
	{Name: "write &>", Tool: "Bash", Command: "make &> README.md", Want: Deny},
	{Name: "write heredoc into file", Tool: "Bash", Command: "cat > README.md <<'EOF'\n# New\nEOF", Want: Deny},
	{Name: "write tee -a", Tool: "Bash", Command: "echo x | tee -a docs/guide.md", Want: Deny},
	{Name: "write cp", Tool: "Bash", Command: "cp notes.txt README.md", Want: Deny},
	{Name: "write mv", Tool: "Bash", Command: "mv notes.txt README.md", Want: Deny},
	{Name: "write install", Tool: "Bash", Command: "install -m 644 notes.txt README.md", Want: Deny},
	{Name: "write dd of=", Tool: "Bash", Command: "dd if=notes.txt of=README.md", Want: Deny},
	{Name: "write truncate", Tool: "Bash", Command: "truncate -s 0 README.md", Want: Deny},
	{Name: "write find -exec sed -i", Tool: "Bash", Command: "find . -name '*.md' -exec sed -i s/one/two/ {} +", Want: Deny},
	{Name: "list names piped to grep", Tool: "Bash", Command: "find . -name '*.md' | grep guide", Want: Allow},
	// Edit, MultiEdit and Write tools.
	{Name: "Edit existing", Tool: "Edit", File: "docs/guide.md", Edits: []pair{{Old: "Install the tool", New: "Set up the tool"}}, Want: Deny},
	{Name: "Edit dash text", Tool: "Edit", File: "docs/guide.md", Edits: []pair{{Old: "-> done", New: "-> finished"}}, Want: Deny},
	{Name: "Edit across blocks", Tool: "Edit", File: "docs/guide.md", Edits: []pair{{Old: "## Setup\n\nInstall", New: "## Install\n\nInstall"}}, Want: Deny},
	{Name: "MultiEdit existing", Tool: "MultiEdit", File: "docs/guide.md", Edits: []pair{{Old: "Install the tool", New: "Set up the tool\nfirst"}, {Old: "| Alpha | one |", New: "| Alpha | two |"}}, Want: Deny},
	{Name: "Write overwrite", Tool: "Write", File: "README.md", Want: Deny},
	{Name: "Write new", Tool: "Write", File: "docs/new.md", Want: Allow},
	{Name: "Edit non-Markdown", Tool: "Edit", File: "main.go", Edits: []pair{{Old: "main", New: "app"}}, Want: Allow},
	// Allowed shapes.
	{Name: "count grep -c", Tool: "Bash", Command: "grep -c install docs/guide.md", Want: Allow},
	{Name: "count wc", Tool: "Bash", Command: "wc -l docs/guide.md README.md", Want: Allow},
	{Name: "count read piped to wc", Tool: "Bash", Command: "cat docs/guide.md | wc -l", Want: Allow},
	{Name: "checksum", Tool: "Bash", Command: "shasum -a 256 README.md && md5sum docs/guide.md", Want: Allow},
	{Name: "stat ls find", Tool: "Bash", Command: "stat README.md; ls docs; find . -name '*.md'", Want: Allow},
	{Name: "git diff", Tool: "Bash", Command: "git diff -- README.md", Want: Allow},
	{Name: "git show rev:path", Tool: "Bash", Command: "git show HEAD~1:docs/guide.md", Want: Allow},
	{Name: "git log", Tool: "Bash", Command: "git log -p -- README.md", Want: Allow},
	{Name: "heredoc data", Tool: "Bash", Command: "cat > notes.txt <<'EOF'\ncat docs/guide.md > README.md\nEOF", Want: Allow},
	{Name: "quoted text", Tool: "Bash", Command: `grep -n "README.md" main.go && git commit -m "edit README.md"`, Want: Allow},
	{Name: "mdnav itself", Tool: "Bash", Command: "mdnav get install docs/guide.md | head -5", Want: Allow},
	{Name: "create new md", Tool: "Bash", Command: "cat > docs/new.md <<'EOF'\n# New\nEOF", Want: Allow},
	{Name: "search off Markdown", Tool: "Bash", Command: "grep -rn --include='*.go' install . && rg -g '!*.md' install && git grep install -- ':!*.md'", Want: Allow},
	{Name: "search a tree without Markdown", Tool: "Bash", Command: "grep -rn install internal && rg install internal", Want: Allow},
	{Name: "search a pipe", Tool: "Bash", Command: "go doc fmt | grep Print && ls | rg md", Want: Allow},
	{Name: "not Markdown", Tool: "Bash", Command: "cat main.go && sed -i 's/a/b/' notes.txt && echo x > out.txt", Want: Allow},
	{Name: "escape comment", Tool: "Bash", Command: "head -c 3 README.md | od -c # mdnav-cannot: need the raw bytes of the file start", Want: Escape},
	{Name: "escape too vague", Tool: "Bash", Command: "cat README.md # mdnav-cannot: faster", Want: Deny},
	{Name: "warn mode", Tool: "Bash", Command: "cat README.md", Warn: true, Want: Warn},
	// apply_patch (Codex, opencode).
	{Name: "patch update", Agents: patchAgents, Tool: "Patch", Patch: "*** Begin Patch\n*** Update File: docs/guide.md\n@@\n-Install the tool\n+Set up the tool\n*** End Patch", Want: Deny},
	{Name: "patch add over existing", Agents: patchAgents, Tool: "Patch", Patch: "*** Begin Patch\n*** Add File: README.md\n+# New\n*** End Patch", Want: Deny},
	{Name: "patch move onto existing", Agents: patchAgents, Tool: "Patch", Patch: "*** Begin Patch\n*** Update File: notes.txt\n*** Move to: README.md\n*** End Patch", Want: Deny},
	{Name: "patch add new and delete", Agents: patchAgents, Tool: "Patch", Patch: "*** Begin Patch\n*** Add File: docs/new.md\n+# New\n*** Delete File: README.md\n*** Update File: main.go\n@@\n-package main\n+package app\n*** End Patch", Want: Allow},
	// Fail closed: a shell or write call the read layer cannot read.
	{Name: "unreadable shell", Tool: "Bash", Bad: true, Want: Deny},
	{Name: "unreadable edit", Tool: "Edit", Bad: true, Want: Deny},
	{Name: "unreadable patch", Agents: patchAgents, Tool: "Patch", Patch: "Update README.md", Want: Deny},
}

var patchAgents = []string{AgentCodex, AgentOpencode}

// Input returns the hook input of a case run in dir, as agent sends it.
func (c Case) Input(agent, dir string) ([]byte, error) {
	oc := agent == AgentOpencode
	key := func(claude, opencode string) string {
		if oc {
			return opencode
		}
		return claude
	}
	tool := map[string]string{"Bash": "Bash", "Edit": "Edit", "MultiEdit": "MultiEdit", "Write": "Write", "Patch": "apply_patch"}[c.Tool]
	if oc {
		tool = map[string]string{"Bash": "bash", "Edit": "edit", "MultiEdit": "multiedit", "Write": "write", "Patch": "apply_patch"}[c.Tool]
	}
	if agent == AgentAgy {
		return c.agyInput(dir)
	}
	file := filepath.Join(dir, c.File)
	var ti any
	switch {
	case c.Bad:
		ti = map[string]any{"command": 5, key("file_path", "filePath"): 5}
	case c.Tool == "Bash":
		ti = map[string]any{"command": c.Command}
	case c.Tool == "Write":
		ti = map[string]any{key("file_path", "filePath"): file, "content": "# Replaced\n"}
	case c.Tool == "Edit":
		ti = map[string]any{key("file_path", "filePath"): file, key("old_string", "oldString"): c.Edits[0].Old, key("new_string", "newString"): c.Edits[0].New}
	case c.Tool == "MultiEdit":
		var edits []map[string]any
		for _, e := range c.Edits {
			edits = append(edits, map[string]any{key("old_string", "oldString"): e.Old, key("new_string", "newString"): e.New})
		}
		ti = map[string]any{key("file_path", "filePath"): file, "edits": edits}
	case c.Tool == "Patch":
		ti = map[string]any{key("input", "patchText"): c.Patch}
	default:
		return nil, fmt.Errorf("case tool %q", c.Tool)
	}
	if oc {
		return json.Marshal(map[string]any{"tool": tool, "args": ti, "cwd": dir, "sessionID": "case"})
	}
	if agent == AgentAgy {
		return json.Marshal(map[string]any{"toolCall": map[string]any{"name": tool, "args": ti},
			"stepIdx": 0, "conversationId": "case", "workspacePaths": []string{dir}})
	}
	return json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "session_id": "case", "tool_name": tool, "tool_input": ti, "cwd": dir})
}

// agyInput renders a case as Antigravity sends it.
func (c Case) agyInput(dir string) ([]byte, error) {
	file := filepath.Join(dir, c.File)
	var name string
	var args any
	switch {
	case c.Bad:
		name, args = map[string]string{"Bash": "run_command", "Edit": "replace_file_content"}[c.Tool], map[string]any{"CommandLine": 5, "TargetFile": 5}
	case c.Tool == "Bash":
		name, args = "run_command", map[string]any{"CommandLine": c.Command, "Cwd": dir, "WaitMsBeforeAsync": 5000}
	case c.Tool == "Edit":
		name, args = "replace_file_content", map[string]any{"TargetFile": file, "TargetContent": c.Edits[0].Old, "ReplacementContent": c.Edits[0].New}
	case c.Tool == "MultiEdit":
		var chunks []map[string]any
		for _, e := range c.Edits {
			chunks = append(chunks, map[string]any{"TargetContent": e.Old, "ReplacementContent": e.New})
		}
		name, args = "multi_replace_file_content", map[string]any{"TargetFile": file, "ReplacementChunks": chunks}
	case c.Tool == "Write":
		name, args = "write_to_file", map[string]any{"TargetFile": file, "CodeContent": "# Replaced\n", "Overwrite": true}
	default:
		return nil, fmt.Errorf("case tool %q has no Antigravity form", c.Tool)
	}
	return json.Marshal(map[string]any{"toolCall": map[string]any{"name": name, "args": args},
		"stepIdx": 0, "conversationId": "case", "workspacePaths": []string{dir}})
}

// WriteFixture writes the case fixture into dir.
func WriteFixture(dir string) error {
	for name, body := range Fixture {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Runner runs mdnav with args in the current directory and returns its
// exit status.
type Runner func(args []string, stdout, stderr io.Writer) int

// Check runs a case in dir, which holds a fresh fixture and is the current
// directory, and verifies the verdict. For a denial it also runs the mdnav
// command the message names and requires it to succeed as written.
func Check(c Case, agent, dir string, run Runner) error {
	raw, err := c.Input(agent, dir)
	if err != nil {
		return err
	}
	ev, err := read(agent, raw)
	if err != nil {
		return err
	}
	out := Evaluate(ev, c.Warn, time.Unix(0, 0))
	if out.Verdict != c.Want {
		return fmt.Errorf("verdict %s, want %s\n%s", out.Verdict, c.Want, out.Reason)
	}
	if ev.Unread != "" {
		if !strings.Contains(out.Reason, ev.Tool) {
			return fmt.Errorf("fail-closed denial does not name the tool %s: %s", ev.Tool, out.Reason)
		}
		return nil
	}
	switch out.Verdict {
	case Allow:
		if out.Log != "" {
			return fmt.Errorf("allow wrote a log line: %q", out.Log)
		}
		return nil
	case Warn, Escape:
		if !strings.HasSuffix(out.Log, "\n") || strings.Count(out.Log, "\n") != 1 || !strings.Contains(out.Log, dir) ||
			!strings.HasSuffix(out.Log, "\t"+agent+"\n") {
			return fmt.Errorf("log line %q: want one line with the cwd, ending in the agent", out.Log)
		}
		if out.Verdict == Escape {
			return nil
		}
	}
	if !strings.Contains(out.Reason, "Run: "+out.Command+"\n") {
		return fmt.Errorf("message does not name the command %q:\n%s", out.Command, out.Reason)
	}
	return runCommand(out.Command, run)
}

// runCommand runs each simple command of a suggested mdnav command line.
func runCommand(line string, run Runner) error {
	sc, err := shell.Parse(line)
	if err != nil {
		return fmt.Errorf("suggested command %q: %w", line, err)
	}
	if len(sc.Commands) == 0 {
		return fmt.Errorf("no suggested command")
	}
	for _, cmd := range sc.Commands {
		if len(cmd.Words) < 2 || cmd.Words[0] != "mdnav" || len(cmd.Redirs) > 0 {
			return fmt.Errorf("suggested command %q is not a plain mdnav command", line)
		}
		var stderr strings.Builder
		if code := run(cmd.Words[1:], io.Discard, &stderr); code != 0 {
			return fmt.Errorf("suggested command %q exited %d: %s", line, code, stderr.String())
		}
	}
	return nil
}

// SelfTest runs every case for every agent it names in a fresh fixture,
// then replays the recorded fixtures under fixtures, and reports each on w.
func SelfTest(w io.Writer, run Runner, fixtures string) error {
	home, err := os.Getwd()
	if err != nil {
		return err
	}
	var failed []string
	n := 0
	for _, c := range Cases {
		for _, agent := range c.Runs() {
			n++
			err := func() error {
				dir, err := os.MkdirTemp("", "mdnav-hook-selftest-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(dir)
				if err := WriteFixture(dir); err != nil {
					return err
				}
				if err := os.Chdir(dir); err != nil {
					return err
				}
				defer os.Chdir(home)
				return Check(c, agent, dir, run)
			}()
			if err != nil {
				failed = append(failed, agent+": "+c.Name)
				fmt.Fprintf(w, "FAIL %-6s %-8s %s: %v\n", c.Want, agent, c.Name, err)
				continue
			}
			fmt.Fprintf(w, "ok   %-6s %-8s %s\n", c.Want, agent, c.Name)
		}
	}
	fmt.Fprintf(w, "hook selftest: %d case runs, %d failed\n", n, len(failed))
	if _, err := os.Stat(fixtures); err != nil {
		fmt.Fprintf(w, "no fixture folder at %s\n", fixtures)
	} else {
		counts, err := ReplayFixtures(w, fixtures)
		if err != nil {
			failed = append(failed, err.Error())
		}
		for _, agent := range Agents {
			fmt.Fprintf(w, "fixtures %-8s %d  fixture-backed: %v\n", agent, counts[agent], fixtureBacked[agent])
		}
	}
	if len(failed) > 0 {
		return errors.New("hook selftest failed: " + strings.Join(failed, ", "))
	}
	return nil
}
