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
		{name: "opencode multiedit per-edit paths", agent: AgentOpencode,
			raw: `{"tool":"multiedit","args":{"edits":[{"filePath":"a.md","oldString":"x","newString":"y"},{"filePath":"b.go","oldString":"p","newString":"q"},{"filePath":"a.md","oldString":"z","newString":"w"}]},"cwd":"/w"}`,
			want: Event{Agent: "opencode", Tool: "multiedit", Kind: KindMultiEdit, Paths: []string{"a.md", "b.go"}, Cwd: "/w",
				Edits: []pair{{Path: "a.md", Old: "x", New: "y"}, {Path: "b.go", Old: "p", New: "q"}, {Path: "a.md", Old: "z", New: "w"}}}},
		{name: "opencode multiedit without paths", agent: AgentOpencode,
			raw:  `{"tool":"multiedit","args":{"edits":[{"oldString":"x","newString":"y"}]}}`,
			want: Event{Agent: "opencode", Tool: "multiedit", Kind: KindMultiEdit, Paths: []string{""}, Edits: []pair{{Old: "x", New: "y"}}, Unread: "no filePath"}},
		{name: "opencode glob", agent: AgentOpencode,
			raw:  `{"tool":"glob","args":{"pattern":"**/*.md"}}`,
			want: Event{Agent: "opencode", Tool: "glob", Kind: KindRead}},
		{name: "agy run_command", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"run_command","args":{"CommandLine":"cat a.md","Cwd":"/w/docs","WaitMsBeforeAsync":5000}},"stepIdx":3,"conversationId":"c","workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "run_command", Kind: KindShell, Command: "cat a.md", Cwd: "/w/docs", Session: "c"}},
		{name: "agy replace_file_content", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"replace_file_content","args":{"TargetFile":"/w/a.md","TargetContent":"x","ReplacementContent":"y","AllowMultiple":true}},"conversationId":"c","workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "replace_file_content", Kind: KindEdit, Paths: []string{"/w/a.md"}, Cwd: "/w", Session: "c", Edits: []pair{{Old: "x", New: "y", All: true}}}},
		{name: "agy multi_replace_file_content", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"multi_replace_file_content","args":{"TargetFile":"a.md","ReplacementChunks":[{"TargetContent":"x","ReplacementContent":"y"}]}},"workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "multi_replace_file_content", Kind: KindMultiEdit, Paths: []string{"a.md"}, Cwd: "/w", Edits: []pair{{Old: "x", New: "y"}}}},
		{name: "agy write_to_file", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"write_to_file","args":{"TargetFile":"/w/a.md","CodeContent":"# A","Overwrite":true}},"workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "write_to_file", Kind: KindWrite, Paths: []string{"/w/a.md"}, Cwd: "/w"}},
		{name: "agy run_command without CommandLine", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"run_command","args":{"Cwd":"/w"}},"workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "run_command", Kind: KindShell, Cwd: "/w", Unread: "no CommandLine"}},
		{name: "agy view_file", agent: AgentAgy,
			raw:  `{"toolCall":{"name":"view_file","args":{"AbsolutePath":"/w/a.md"}},"workspacePaths":["/w"]}`,
			want: Event{Agent: "agy", Tool: "view_file", Kind: KindRead, Cwd: "/w"}},
		{name: "agy without toolCall", agent: AgentAgy, raw: `{"conversationId":"c"}`,
			want: Event{Agent: "agy", Kind: KindOther, Session: "c"}},
		{name: "opencode v2 shell", agent: AgentOpencode,
			raw:  `{"tool":"shell","args":{"command":"cat a.md","workdir":"docs"},"cwd":"/w"}`,
			want: Event{Agent: "opencode", Tool: "shell", Kind: KindShell, Command: "cat a.md", Cwd: "/w/docs"}},
		{name: "opencode v2 edit path", agent: AgentOpencode,
			raw:  `{"tool":"edit","args":{"path":"a.md","oldString":"x","newString":"y"},"cwd":"/w"}`,
			want: Event{Agent: "opencode", Tool: "edit", Kind: KindEdit, Paths: []string{"a.md"}, Cwd: "/w", Edits: []pair{{Old: "x", New: "y"}}}},
		{name: "opencode v2 write path", agent: AgentOpencode,
			raw:  `{"tool":"write","args":{"path":"a.md","content":"x"},"cwd":"/w"}`,
			want: Event{Agent: "opencode", Tool: "write", Kind: KindWrite, Paths: []string{"a.md"}, Cwd: "/w"}},
		{name: "unknown agent", agent: "cursor", raw: `{}`, err: true},
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
		{AgentOpencode, deny, 0, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"use mdnav"}}` + "\n"},
		{AgentAgy, deny, 0, `{"decision":"deny","reason":"use mdnav"}` + "\n"},
		{AgentAgy, Outcome{Verdict: Allow}, 0, `{"decision":"allow"}` + "\n"},
		{"cursor", deny, 2, ""},
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

// TestRecordRedacts is the recording table: what a recording must drop and
// what it must keep.
func TestRecordRedacts(t *testing.T) {
	rows := []struct {
		name, raw  string
		drop, keep []string
	}{
		{"env values", `{"toolCall":{"env":{"API_KEY":"s3cret-value","HOME":"/h"},"command":"ls"}}`,
			[]string{"s3cret-value", `"/h"`}, []string{`"API_KEY"`, `"ls"`, "mdnav_record_redacted"}},
		{"bearer header", `{"tool_input":{"command":"curl -H 'Authorization: Bearer abcdef0123456789abcdef' x"}}`,
			[]string{"abcdef0123456789abcdef"}, []string{"curl -H", "mdnav_record_redacted"}},
		{"token assignment", `{"tool_input":{"command":"GITHUB_TOKEN=ghp_0123456789abcdefghij0123 gh pr list"}}`,
			[]string{"ghp_0123456789abcdefghij0123"}, []string{"GITHUB_TOKEN=", "gh pr list"}},
		{"api key shapes", `{"a":"sk-ant-0123456789abcdefghij","b":"AKIA0123456789ABCDEF","c":"xoxb-0123456789-abc"}`,
			[]string{"sk-ant-0123456789abcdefghij", "AKIA0123456789ABCDEF", "xoxb-0123456789-abc"}, nil},
		{"plain call kept whole", `{"tool_input":{"command":"cat docs/guide.md"}}`,
			[]string{"mdnav_record_redacted"}, []string{"cat docs/guide.md"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			p, err := Record(filepath.Join(t.TempDir(), "rec"), AgentClaude, []byte(r.raw), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			for _, s := range r.drop {
				if strings.Contains(string(got), s) {
					t.Errorf("recording keeps %q:\n%s", s, got)
				}
			}
			for _, s := range r.keep {
				if !strings.Contains(string(got), s) {
					t.Errorf("recording lost %q:\n%s", s, got)
				}
			}
		})
	}
}

// TestInstallFlags runs --print-install, --check-install and a warn-mode
// log into a directory that does not exist yet through Main.
func TestInstallFlags(t *testing.T) {
	home := t.TempDir()
	log := filepath.Join(home, ".local", "state", "mdnav", "claude-mdnav-cannot.log")
	vars := map[string]string{"HOME": home, "MDNAV_CANNOT_LOG": log}
	WriteFixture(home)
	t.Chdir(home)
	rows := []struct {
		name  string
		args  []string
		stdin string
		code  int
		out   string
	}{
		{"print claude", []string{"--print-install"}, "", 0, `"matcher": "Bash"`},
		{"print agy", []string{"--print-install", "--agent", "agy"}, "", 0, "mdnav refuses --agent agy"},
		{"check codex json", []string{"--check-install", "--agent", "codex", "--json"}, "", 1, `"agent":"codex"`},
		{"json alone", []string{"--json"}, "", 2, ""},
		{"warn log in a new dir", nil, `{"tool_name":"Bash","tool_input":{"command":"cat README.md"},"cwd":` + quote(home) + `}`, 0, ""},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Main(r.args, Env{Stdin: strings.NewReader(r.stdin), Stdout: &out, Stderr: &errb,
				Getenv: func(k string) string { return vars[k] }, Now: func() time.Time { return time.Unix(0, 0) }})
			if code != r.code || !strings.Contains(out.String(), r.out) {
				t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, r.code, out.String(), errb.String())
			}
		})
	}
	if b, err := os.ReadFile(log); err != nil || !strings.HasSuffix(string(b), "\tclaude\n") {
		t.Fatalf("log %q: %v", b, err)
	}
}

// TestMultiEditPaths judges a multiedit across files: the Markdown file's
// edits decide, the Go file's do not.
func TestMultiEditPaths(t *testing.T) {
	dir := t.TempDir()
	WriteFixture(dir)
	t.Chdir(dir)
	for _, r := range []struct {
		files []string
		want  string
	}{{[]string{"main.go", "README.md"}, Deny}, {[]string{"main.go", "notes.txt"}, Allow}} {
		args := map[string]any{"edits": []map[string]any{
			{"filePath": r.files[0], "oldString": "main", "newString": "app"},
			{"filePath": r.files[1], "oldString": "See", "newString": "Read"},
		}}
		raw, _ := json.Marshal(map[string]any{"tool": "multiedit", "args": args, "cwd": dir})
		ev, err := read(AgentOpencode, raw)
		if err != nil {
			t.Fatal(err)
		}
		if out := Evaluate(ev, false, time.Unix(0, 0)); out.Verdict != r.want || r.want == Deny && !strings.Contains(out.Reason, "Run: mdnav sub README.md:3 See Read") {
			t.Fatalf("%v: verdict %s, want %s\n%s", r.files, out.Verdict, r.want, out.Reason)
		}
	}
}
