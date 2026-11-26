package hook

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestRead is the read-layer table: one agent input per row and the Event
// it reads as.
func TestRead(t *testing.T) {
	const patch = "*** Begin Patch\n*** Update File: a.md\n*** Move to: b.md\n*** Add File: c.md\n+x\n*** Delete File: d.md\n*** End Patch"
	rows := []struct {
		name, agent, raw string
		want             Event
		err              bool
	}{
		{name: "claude bash", agent: AgentClaude,
			raw:  `{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"Bash","tool_input":{"command":"cat a.md"},"cwd":"/w"}`,
			want: Event{Agent: "claude", Event: "PreToolUse", Tool: "Bash", Kind: KindShell, Command: "cat a.md", Cwd: "/w", Session: "s"}},
		{name: "claude multiedit", agent: AgentClaude,
			raw:  `{"tool_name":"MultiEdit","tool_input":{"file_path":"a.md","edits":[{"old_string":"x","new_string":"y","replace_all":true}]},"cwd":"/w"}`,
			want: Event{Agent: "claude", Tool: "MultiEdit", Kind: KindMultiEdit, Paths: []string{"a.md"}, Cwd: "/w", Edits: []pair{{Old: "x", New: "y", All: true}}}},
		{name: "claude read tool", agent: AgentClaude,
			raw:  `{"tool_name":"Read","tool_input":{"file_path":"a.md"}}`,
			want: Event{Agent: "claude", Tool: "Read", Kind: KindRead}},
		{name: "claude unknown tool", agent: AgentClaude,
			raw:  `{"tool_name":"mcp__x__y","tool_input":7}`,
			want: Event{Agent: "claude", Tool: "mcp__x__y", Kind: KindOther}},
		{name: "claude write without path", agent: AgentClaude,
			raw:  `{"tool_name":"Write","tool_input":{"content":"x"}}`,
			want: Event{Agent: "claude", Tool: "Write", Kind: KindWrite, Paths: []string{""}, Unread: "no file_path"}},
		{name: "codex apply_patch string", agent: AgentCodex,
			raw: `{"tool_name":"apply_patch","tool_input":` + quote(patch) + `,"cwd":"/w"}`,
			want: Event{Agent: "codex", Tool: "apply_patch", Kind: KindPatch, Cwd: "/w", Paths: []string{"a.md", "b.md", "c.md", "d.md"},
				Patch: []patchOp{{Op: "update", Path: "a.md", To: "b.md"}, {Op: "add", Path: "c.md"}, {Op: "delete", Path: "d.md"}}}},
		{name: "codex bash bad", agent: AgentCodex,
			raw:  `{"tool_name":"Bash","tool_input":{"cmd":"ls"}}`,
			want: Event{Agent: "codex", Tool: "Bash", Kind: KindShell, Unread: "no command"}},
		{name: "opencode bash workdir", agent: AgentOpencode,
			raw:  `{"tool":"bash","args":{"command":"cat a.md","workdir":"docs"},"cwd":"/w","sessionID":"s"}`,
			want: Event{Agent: "opencode", Tool: "bash", Kind: KindShell, Command: "cat a.md", Cwd: "/w/docs", Session: "s"}},
		{name: "opencode edit", agent: AgentOpencode,
			raw:  `{"tool":"edit","args":{"filePath":"/w/a.md","oldString":"x","newString":"y"}}`,
			want: Event{Agent: "opencode", Tool: "edit", Kind: KindEdit, Paths: []string{"/w/a.md"}, Edits: []pair{{Old: "x", New: "y"}}}},
		{name: "opencode patch without begin", agent: AgentOpencode,
			raw:  `{"tool":"apply_patch","args":{"patchText":"*** Update File: a.md"}}`,
			want: Event{Agent: "opencode", Tool: "apply_patch", Kind: KindPatch, Unread: "no *** Begin Patch line"}},
		{name: "opencode glob", agent: AgentOpencode,
			raw:  `{"tool":"glob","args":{"pattern":"**/*.md"}}`,
			want: Event{Agent: "opencode", Tool: "glob", Kind: KindRead}},
		{name: "agy has no read layer", agent: AgentAgy, raw: `{}`, err: true},
		{name: "not JSON", agent: AgentClaude, raw: `{`, err: true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got, err := read(r.agent, []byte(r.raw))
			if (err != nil) != r.err {
				t.Fatalf("err %v, want error %v", err, r.err)
			}
			if !r.err && !reflect.DeepEqual(got, r.want) {
				t.Fatalf("got  %+v\nwant %+v", got, r.want)
			}
		})
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestWrite is the write-layer table: a denial and an allow per agent.
func TestWrite(t *testing.T) {
	deny := Outcome{Verdict: Deny, Reason: "use mdnav"}
	rows := []struct {
		agent string
		out   Outcome
		code  int
		want  string
	}{
		{AgentClaude, deny, 0, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"use mdnav"}}` + "\n"},
		{AgentCodex, deny, 0, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"use mdnav"}}` + "\n"},
		{AgentOpencode, deny, 0, `{"decision":"deny","reason":"use mdnav"}` + "\n"},
		{AgentAgy, deny, 2, ""},
		{AgentClaude, Outcome{Verdict: Warn, Reason: "x"}, 0, ""},
		{AgentOpencode, Outcome{Verdict: Allow}, 0, ""},
	}
	for _, r := range rows {
		t.Run(r.agent+"/"+r.out.Verdict, func(t *testing.T) {
			var b bytes.Buffer
			code, _ := Write(r.agent, r.out, &b)
			if code != r.code || b.String() != r.want {
				t.Fatalf("exit %d output %q, want %d %q", code, b.String(), r.code, r.want)
			}
		})
	}
}

// TestMainCLI is the CLI table: flags, the fixture gate and --record.
func TestMainCLI(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "rec")
	open := t.TempDir()
	long := strings.Repeat("é", maxContent)
	bash := `{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/w"}`
	rows := []struct {
		name  string
		args  []string
		stdin string
		code  int
		err   string
	}{
		{"unknown agent", []string{"--agent", "cursor"}, bash, 2, `unknown agent "cursor"`},
		{"unknown flag", []string{"--nope"}, bash, 2, "unknown argument"},
		{"default claude", nil, bash, 0, ""},
		{"codex refused", []string{"--agent=codex"}, bash, 2, "no fixture-backed format for codex"},
		{"opencode refused", []string{"--agent", "opencode"}, `{"tool":"bash","args":{"command":"ls"}}`, 2, "no fixture-backed format for opencode"},
		{"agy refused", []string{"--agent", "agy"}, `{}`, 2, "no fixture-backed format for agy"},
		{"agy records", []string{"--agent", "agy", "--record", rec}, `{"toolCall":{"id":"1","content":"` + long + `"},"cwd":"/w"}`, 0, ""},
		{"record dir open to others", []string{"--record", open}, bash, 1, "open to other users"},
		{"bad input", nil, `{`, 1, "cannot read the hook input"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Main(r.args, Env{Stdin: strings.NewReader(r.stdin), Stdout: &out, Stderr: &errb,
				Getenv: func(string) string { return "" }, Now: func() time.Time { return time.Unix(0, 0) }})
			if code != r.code || !strings.Contains(errb.String(), r.err) {
				t.Fatalf("exit %d stderr %q, want %d containing %q", code, errb.String(), r.code, r.err)
			}
		})
	}

	files, _ := filepath.Glob(filepath.Join(rec, "agy-*.json"))
	if len(files) != 1 {
		t.Fatalf("recordings %v, want one", files)
	}
	for p, want := range map[string]os.FileMode{rec: 0o700, files[0]: 0o600} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != want {
			t.Fatalf("%s: mode %v, want %v (%v)", p, fi.Mode().Perm(), want, err)
		}
	}
	var got struct {
		ToolCall struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"toolCall"`
		Cwd       string `json:"cwd"`
		Truncated bool   `json:"mdnav_record_truncated"`
	}
	raw, _ := os.ReadFile(files[0])
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCall.Content) > maxContent || len(got.ToolCall.Content) < maxContent-1 || !got.Truncated || got.ToolCall.ID != "1" || got.Cwd != "/w" {
		t.Fatalf("recording: content %d bytes, truncated %v, id %q, cwd %q", len(got.ToolCall.Content), got.Truncated, got.ToolCall.ID, got.Cwd)
	}
}

// TestReplayFixtures replays a fixture folder: a labelled fixture that
// passes, one that fails, and an unlabelled one.
func TestReplayFixtures(t *testing.T) {
	dir := t.TempDir()
	put := func(name, body string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("claude/edit.json", `{"tool_name":"Edit","tool_input":{"file_path":"/rec/README.md","old_string":"See","new_string":"Read"},"cwd":"/rec"}`)
	put("claude/edit.want", "deny edit\n")
	put("codex/cat.json", `{"tool_name":"Bash","tool_input":{"command":"cat /rec/notes.txt"},"cwd":"/rec"}`)
	put("codex/cat.want", "deny shell\n")
	put("opencode/new.json", `{"tool":"write","args":{"filePath":"x.md"}}`)
	var out bytes.Buffer
	counts, err := ReplayFixtures(&out, dir)
	if err == nil || !strings.Contains(err.Error(), "codex/cat.json") || strings.Contains(err.Error(), "claude/") {
		t.Fatalf("err %v, want only codex/cat.json failing\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ok   fixture claude/edit.json") || !strings.Contains(out.String(), "new  fixture opencode/new.json") {
		t.Fatalf("report:\n%s", out.String())
	}
	if counts[AgentClaude] != 1 || counts[AgentAgy] != 0 {
		t.Fatalf("counts %v", counts)
	}
	if !errors.Is(func() error { _, err := Read(AgentCodex, nil); return err }(), ErrNoFixtures) {
		t.Fatal("codex is not gated")
	}
}
