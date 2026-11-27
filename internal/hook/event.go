package hook

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Agents mdnav hook reads.
const (
	AgentClaude   = "claude"
	AgentCodex    = "codex"
	AgentOpencode = "opencode"
	AgentAgy      = "agy"
)

// Agents lists every agent --agent accepts, in help order.
var Agents = []string{AgentClaude, AgentCodex, AgentOpencode, AgentAgy}

// fixtureBacked lists the agents whose read layer is backed by recorded
// fixtures. Every other agent is refused until its recordings are in the
// fixture folder and it is added here.
var fixtureBacked = map[string]bool{AgentClaude: true, AgentCodex: true, AgentOpencode: true}

// Tool kinds of an Event.
const (
	KindShell     = "shell"
	KindEdit      = "edit"
	KindWrite     = "write"
	KindMultiEdit = "multi_edit"
	KindPatch     = "patch"
	KindRead      = "read"
	KindOther     = "other"
)

// Event is one tool call in the agent-independent form the rules judge.
type Event struct {
	Agent   string
	Event   string // the agent's hook event name, "" when it sends none
	Tool    string // the agent's own tool name, for messages and the log
	Kind    string
	Command string   // shell
	Paths   []string // edit, write, multi_edit, patch: files as written
	Cwd     string
	Session string
	Edits   []pair    // edit (one), multi_edit
	Patch   []patchOp // patch
	Unread  string    // a shell or write call whose input could not be read: why
}

// ErrNoFixtures refuses an agent whose format has no recorded fixtures.
var ErrNoFixtures = errors.New("no fixture-backed format")

// Read turns an agent's raw hook input into an Event. It refuses agents
// without fixtures. An error means the input as a whole is unreadable; a
// tool call it cannot read comes back as an Event with Unread set.
func Read(agent string, raw []byte) (Event, error) {
	if !fixtureBacked[agent] {
		return Event{}, fmt.Errorf("%w for %s", ErrNoFixtures, agent)
	}
	return read(agent, raw)
}

// read is Read without the fixture gate, for the tests and the fixture
// replay that decide whether an agent can be added to fixtureBacked.
func read(agent string, raw []byte) (Event, error) {
	switch agent {
	case AgentClaude, AgentCodex:
		return readClaude(agent, raw)
	case AgentOpencode:
		return readOpencode(raw)
	case AgentAgy:
		return readAgy(raw)
	}
	return Event{}, fmt.Errorf("%w for %s", ErrNoFixtures, agent)
}

// readClaude reads the Claude Code PreToolUse payload, which Codex shares.
func readClaude(agent string, raw []byte) (Event, error) {
	var in struct {
		HookEventName string         `json:"hook_event_name"`
		SessionID     string         `json:"session_id"`
		ToolName      string         `json:"tool_name"`
		ToolInput     jsontext.Value `json:"tool_input"`
		Cwd           string         `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, err
	}
	ev := Event{Agent: agent, Event: in.HookEventName, Tool: in.ToolName, Cwd: in.Cwd, Session: in.SessionID}
	ti := in.ToolInput
	var err error
	switch in.ToolName {
	case "Bash":
		ev.Kind = KindShell
		var a struct {
			Command *string `json:"command"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.Command != nil, "command")
		}
		if err == nil {
			ev.Command = *a.Command
		}
	case "Edit":
		ev.Kind = KindEdit
		var a struct {
			FilePath   string `json:"file_path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.FilePath != "", "file_path")
		}
		ev.Paths = []string{a.FilePath}
		ev.Edits = []pair{{Old: a.OldString, New: a.NewString, All: a.ReplaceAll}}
	case "MultiEdit":
		ev.Kind = KindMultiEdit
		var a struct {
			FilePath string `json:"file_path"`
			Edits    []struct {
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			} `json:"edits"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.FilePath != "", "file_path")
		}
		ev.Paths = []string{a.FilePath}
		for _, e := range a.Edits {
			ev.Edits = append(ev.Edits, pair{Old: e.OldString, New: e.NewString, All: e.ReplaceAll})
		}
	case "Write", "NotebookEdit":
		ev.Kind = KindWrite
		var a struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		if err = decode(ti, &a); err == nil {
			ev.Paths = []string{a.FilePath + a.NotebookPath}
			err = need(ev.Paths[0] != "", "file_path")
		}
	case "apply_patch":
		ev.Kind = KindPatch
		ev.Patch, err = patchInput(ti)
		ev.Paths = patchPaths(ev.Patch)
	case "Read", "Glob", "Grep", "LS", "NotebookRead", "WebFetch", "WebSearch":
		ev.Kind = KindRead
	default:
		ev.Kind = KindOther
	}
	if err != nil {
		ev.Unread = err.Error()
	}
	return ev, nil
}

// patchInput finds the patch text of a Codex apply_patch call: the input
// itself when it is a string, else its input, patch or command field.
func patchInput(ti jsontext.Value) ([]patchOp, error) {
	var text string
	if json.Unmarshal(ti, &text) != nil {
		var a struct {
			Input   string `json:"input"`
			Patch   string `json:"patch"`
			Command string `json:"command"`
		}
		if err := decode(ti, &a); err != nil {
			return nil, err
		}
		text = a.Input + a.Patch + a.Command
	}
	return parsePatch(text)
}

// readOpencode reads what the opencode plugin sends: opencode's own tool
// name and arguments.
func readOpencode(raw []byte) (Event, error) {
	var in struct {
		Tool      string         `json:"tool"`
		Args      jsontext.Value `json:"args"`
		Cwd       string         `json:"cwd"`
		SessionID string         `json:"sessionID"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, err
	}
	ev := Event{Agent: AgentOpencode, Tool: in.Tool, Cwd: in.Cwd, Session: in.SessionID}
	ti := in.Args
	var err error
	switch in.Tool {
	case "bash", "shell": // V1 bash, V2 shell
		ev.Kind = KindShell
		var a struct {
			Command *string `json:"command"`
			Workdir string  `json:"workdir"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.Command != nil, "command")
		}
		if err == nil {
			ev.Command = *a.Command
			if a.Workdir != "" {
				ev.Cwd = resolve(ev.Cwd, a.Workdir)
			}
		}
	case "edit", "multiedit":
		ev.Kind = KindEdit
		if in.Tool == "multiedit" {
			ev.Kind = KindMultiEdit
		}
		type spec struct {
			FilePath   string `json:"filePath"`
			Path       string `json:"path"` // V2
			OldString  string `json:"oldString"`
			NewString  string `json:"newString"`
			ReplaceAll bool   `json:"replaceAll"`
		}
		var a struct {
			spec
			Edits []spec `json:"edits"`
		}
		err = decode(ti, &a)
		if a.FilePath == "" {
			a.FilePath = a.Path
		}
		specs := a.Edits
		if ev.Kind == KindEdit {
			specs = []spec{a.spec}
		}
		// A multiedit names one filePath for all edits, or one per edit.
		for _, s := range specs {
			p := pair{Old: s.OldString, New: s.NewString, All: s.ReplaceAll}
			if s.FilePath == "" {
				s.FilePath = s.Path
			}
			if a.FilePath == "" && s.FilePath != "" {
				p.Path = s.FilePath
				if !slices.Contains(ev.Paths, s.FilePath) {
					ev.Paths = append(ev.Paths, s.FilePath)
				}
			}
			ev.Edits = append(ev.Edits, p)
		}
		if a.FilePath != "" || len(ev.Paths) == 0 {
			ev.Paths = []string{a.FilePath}
		}
		if err == nil {
			err = need(!slices.Contains(ev.Paths, ""), "filePath")
		}
	case "write":
		ev.Kind = KindWrite
		var a struct {
			FilePath string `json:"filePath"`
			Path     string `json:"path"` // V2
		}
		if err = decode(ti, &a); err == nil {
			ev.Paths = []string{a.FilePath + a.Path}
			err = need(ev.Paths[0] != "", "filePath")
		}
	case "patch", "apply_patch":
		ev.Kind = KindPatch
		var a struct {
			PatchText string `json:"patchText"`
		}
		if err = decode(ti, &a); err == nil {
			ev.Patch, err = parsePatch(a.PatchText)
			ev.Paths = patchPaths(ev.Patch)
		}
	case "read", "glob", "grep", "list", "ls", "lsp", "webfetch", "websearch", "todoread", "todowrite", "task", "question", "skill":
		ev.Kind = KindRead
	default:
		ev.Kind = KindOther
	}
	if err != nil {
		ev.Unread = err.Error()
	}
	return ev, nil
}

// readAgy reads an Antigravity PreToolUse payload: toolCall {name, args},
// conversationId and workspacePaths, with Antigravity's tool and argument
// names.
func readAgy(raw []byte) (Event, error) {
	var in struct {
		HookEventName string `json:"hookEventName"`
		ToolCall      struct {
			Name string         `json:"name"`
			Args jsontext.Value `json:"args"`
		} `json:"toolCall"`
		ConversationID string   `json:"conversationId"`
		WorkspacePaths []string `json:"workspacePaths"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, err
	}
	ev := Event{Agent: AgentAgy, Event: in.HookEventName, Tool: in.ToolCall.Name, Session: in.ConversationID}
	if len(in.WorkspacePaths) > 0 {
		ev.Cwd = in.WorkspacePaths[0]
	}
	ti := in.ToolCall.Args
	var err error
	switch in.ToolCall.Name {
	case "run_command":
		ev.Kind = KindShell
		var a struct {
			CommandLine *string `json:"CommandLine"`
			Cwd         string  `json:"Cwd"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.CommandLine != nil, "CommandLine")
		}
		if a.Cwd != "" {
			ev.Cwd = resolve(ev.Cwd, a.Cwd)
		}
		if err == nil {
			ev.Command = *a.CommandLine
		}
	case "replace_file_content", "multi_replace_file_content":
		ev.Kind = KindEdit
		if in.ToolCall.Name == "multi_replace_file_content" {
			ev.Kind = KindMultiEdit
		}
		type chunk struct {
			TargetContent      string `json:"TargetContent"`
			ReplacementContent string `json:"ReplacementContent"`
			AllowMultiple      bool   `json:"AllowMultiple"`
		}
		var a struct {
			TargetFile string `json:"TargetFile"`
			chunk
			ReplacementChunks []chunk `json:"ReplacementChunks"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.TargetFile != "", "TargetFile")
		}
		ev.Paths = []string{a.TargetFile}
		chunks := a.ReplacementChunks
		if ev.Kind == KindEdit {
			chunks = []chunk{a.chunk}
		}
		for _, c := range chunks {
			ev.Edits = append(ev.Edits, pair{Old: c.TargetContent, New: c.ReplacementContent, All: c.AllowMultiple})
		}
	case "write_to_file":
		ev.Kind = KindWrite
		var a struct {
			TargetFile string `json:"TargetFile"`
		}
		if err = decode(ti, &a); err == nil {
			err = need(a.TargetFile != "", "TargetFile")
		}
		ev.Paths = []string{a.TargetFile}
	case "view_file", "list_dir", "find_by_name", "grep_search", "search_web", "read_url_content",
		"manage_task", "list_permissions", "ask_permission", "ask_question":
		ev.Kind = KindRead
	default:
		ev.Kind = KindOther
	}
	if err != nil {
		ev.Unread = err.Error()
	}
	return ev, nil
}

func decode(v jsontext.Value, dst any) error {
	if len(v) == 0 {
		return errors.New("no tool input")
	}
	return json.Unmarshal(v, dst)
}

func need(ok bool, field string) error {
	if ok {
		return nil
	}
	return fmt.Errorf("no %s", field)
}

// patchOp is one file operation of an apply_patch patch.
type patchOp struct {
	Op   string // add, update or delete
	Path string
	To   string // update: the "*** Move to:" target, "" for none
}

// parsePatch reads the file operations of an apply_patch patch:
// "*** Add File:", "*** Update File:" (with an optional "*** Move to:")
// and "*** Delete File:" between "*** Begin Patch" and "*** End Patch".
func parsePatch(text string) ([]patchOp, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	begin := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "*** Begin Patch" {
			begin = i
			break
		}
	}
	if begin < 0 {
		return nil, errors.New("no *** Begin Patch line")
	}
	var ops []patchOp
	for _, l := range lines[begin+1:] {
		switch {
		case strings.TrimSpace(l) == "*** End Patch":
			if len(ops) == 0 {
				return nil, errors.New("patch names no file")
			}
			return ops, nil
		case strings.HasPrefix(l, "*** Add File: "):
			ops = append(ops, patchOp{Op: "add", Path: strings.TrimSpace(l[len("*** Add File: "):])})
		case strings.HasPrefix(l, "*** Update File: "):
			ops = append(ops, patchOp{Op: "update", Path: strings.TrimSpace(l[len("*** Update File: "):])})
		case strings.HasPrefix(l, "*** Delete File: "):
			ops = append(ops, patchOp{Op: "delete", Path: strings.TrimSpace(l[len("*** Delete File: "):])})
		case strings.HasPrefix(l, "*** Move to: "):
			if len(ops) == 0 || ops[len(ops)-1].Op != "update" {
				return nil, errors.New("*** Move to: outside an update")
			}
			ops[len(ops)-1].To = strings.TrimSpace(l[len("*** Move to: "):])
		}
	}
	return nil, errors.New("no *** End Patch line")
}

func patchPaths(ops []patchOp) []string {
	var ps []string
	for _, o := range ops {
		ps = append(ps, o.Path)
		if o.To != "" {
			ps = append(ps, o.To)
		}
	}
	return ps
}
