package hook

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OpencodePlugin is the guard plugin opencode loads; the mdnav call goes in it.
const OpencodePlugin = "/Users/odemir/Development/identuum/wiki/tools/opencode/identuum-guard.js"

// guard is one hook command in a tool group.
type guard struct {
	Name    string // mdnav
	Command string
}

// group is one tool group of an agent's registration.
type group struct {
	Name    string // shell or edit
	Matcher string
	Guards  []guard
}

// home returns the user's home directory from getenv.
func home(getenv func(string) string) string {
	if h := getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func stateDir(getenv func(string) string) string {
	return filepath.Join(home(getenv), ".local", "state", "mdnav")
}

// LogPath is the MDNAV_CANNOT_LOG file an install gives agent.
func LogPath(agent string, getenv func(string) string) string {
	return filepath.Join(stateDir(getenv), agent+"-mdnav-cannot.log")
}

func mdnavGuardCommand(agent string, getenv func(string) string) string {
	return "env MDNAV_HOOK_MODE=warn MDNAV_CANNOT_LOG=" + LogPath(agent, getenv) + " mdnav hook --agent " + agent
}

// groups returns agent's mdnav registration for shell and edit calls. agy
// has none until it is measured.
func groups(agent string, getenv func(string) string) ([]group, error) {
	m := guard{"mdnav", mdnavGuardCommand(agent, getenv)}
	shell := []guard{m}
	switch agent {
	case AgentClaude:
		return []group{{"shell", "Bash", shell}, {"edit", "Edit|Write|MultiEdit", []guard{m}}}, nil
	case AgentCodex:
		return []group{{"shell", "^Bash$", shell}}, nil
	case AgentOpencode:
		return []group{{"shell", "bash", shell}, {"edit", "edit|write|multiedit|apply_patch", []guard{m}}}, nil
	}
	return nil, fmt.Errorf("%s is not measured: its hooks.json path, edit tool names and decision words come from fixtures", agent)
}

// configPath is the file agent's registration lives in.
func configPath(agent string, getenv func(string) string) string {
	h := home(getenv)
	switch agent {
	case AgentClaude:
		if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, "settings.json")
		}
		return filepath.Join(h, ".claude", "settings.json")
	case AgentCodex:
		if d := getenv("CODEX_HOME"); d != "" {
			return filepath.Join(d, "config.toml")
		}
		return filepath.Join(h, ".codex", "config.toml")
	case AgentOpencode:
		d := getenv("XDG_CONFIG_HOME")
		if d == "" {
			d = filepath.Join(h, ".config")
		}
		return filepath.Join(d, "opencode", "plugins", "identuum-guard.js")
	}
	return ""
}

// PrintInstall prints agent's complete mdnav registration and returns the
// exit status: 0, or 2 for an agent that is not measured.
func PrintInstall(w io.Writer, agent string, getenv func(string) string) int {
	fmt.Fprintf(w, "# mdnav hook --print-install --agent %s\n", agent)
	record := "mdnav hook --agent " + agent + " --record " + filepath.Join(stateDir(getenv), "record")
	gs, err := groups(agent, getenv)
	if err != nil {
		fmt.Fprintf(w, "# agy is not measured: its hooks.json path, edit tool names and decision words\n"+
			"# come from fixtures. Record first: register\n#   %s\n"+
			"# as agy's PreToolUse command, run one session, and add the recordings to\n# <fixtures>/agy/.\n", record)
		return 2
	}
	unbacked := !fixtureBacked[agent]
	switch agent {
	case AgentClaude:
		fmt.Fprintf(w, "# File: %s (merge into \"hooks\"; keep every other key)\n", configPath(agent, getenv))
		fmt.Fprint(w, "{\n  \"hooks\": {\n    \"PreToolUse\": [\n")
		for i, g := range gs {
			fmt.Fprintf(w, "      {\n        \"matcher\": %s,\n        \"hooks\": [\n", jsonString(g.Matcher))
			for j, gd := range g.Guards {
				fmt.Fprintf(w, "          {\"type\": \"command\", \"command\": %s}%s\n", jsonString(gd.Command), comma(j, len(g.Guards)))
			}
			fmt.Fprintf(w, "        ]\n      }%s\n", comma(i, len(gs)))
		}
		fmt.Fprint(w, "    ]\n  }\n}\n")
	case AgentCodex:
		fmt.Fprintf(w, "# File: %s\n", configPath(agent, getenv))
		fmt.Fprint(w, "# apply_patch: not measured whether Codex sends it to hooks, so there is no\n"+
			"# apply_patch matcher until a fixture shows it.\n"+
			"# Check the keys against the Codex hooks docs; they were not reachable when\n"+
			"# this text was written.\n")
		if unbacked {
			fmt.Fprintf(w, "# mdnav refuses --agent codex (exit 2) until codex has fixtures; until then use\n#   %s\n# as the mdnav command.\n", record)
		}
		fmt.Fprint(w, "[features]\ncodex_hooks = true\n")
		for _, g := range gs {
			fmt.Fprintf(w, "\n[[hooks.PreToolUse]]\nmatcher = %s\n", jsonString(g.Matcher))
			for _, gd := range g.Guards {
				fmt.Fprintf(w, "\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = %s\n", jsonString(gd.Command))
			}
		}
		fmt.Fprint(w, "\n# Owner step: start codex once interactively and trust the new hooks. Lanes run\n"+
			"# non-interactively and skip untrusted hooks.\n")
	case AgentOpencode:
		fmt.Fprintf(w, "# opencode runs no command hooks: the guard plugin calls mdnav.\n# Plugin: %s\n", OpencodePlugin)
		if unbacked {
			fmt.Fprintf(w, "# mdnav refuses --agent opencode (exit 2) until opencode has fixtures, and the\n"+
				"# call below denies on any non-zero exit, blocking every tool. Until then add\n"+
				"# \"--record\", %s to MDNAV_CMD.\n", jsonString(filepath.Join(stateDir(getenv), "record")))
		}
		var words []string
		for _, f := range strings.Fields(mdnavGuardCommand(agent, getenv)) {
			words = append(words, jsonString(f))
		}
		fmt.Fprint(w, opencodeStep1)
		fmt.Fprintf(w, "const MDNAV_CMD = [%s];\n", strings.Join(words, ", "))
		fmt.Fprint(w, opencodeRest)
		fmt.Fprintf(w, "cp %s %s\n", OpencodePlugin, configPath(agent, getenv))
	}
	return 0
}

func comma(i, n int) string {
	if i < n-1 {
		return ","
	}
	return ""
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Install statuses.
const (
	Registered = "registered"
	Missing    = "missing"
	Different  = "different"
)

// entry is one registered matcher and its hook commands, as read from an
// agent's config. Nothing else of the config is kept.
type entry struct {
	Matcher  string
	Commands []string
}

// signature tells whether a command is an mdnav hook.
func signature(cmd string) bool {
	return strings.Contains(cmd, "mdnav hook") || strings.Contains(cmd, `"mdnav", "hook"`)
}

// tools returns the tool names a matcher names; nil means every tool.
func tools(matcher string) map[string]bool {
	m := strings.Trim(matcher, "^$()")
	if m == "" || m == "*" || m == ".*" {
		return nil
	}
	set := map[string]bool{}
	for _, t := range strings.Split(m, "|") {
		set[strings.Trim(t, "^$() ")] = true
	}
	return set
}

func overlaps(a, b map[string]bool) bool {
	if a == nil || b == nil {
		return true
	}
	for t := range a {
		if b[t] {
			return true
		}
	}
	return false
}

// status judges one guard of one group against the registered entries.
func status(g group, gd guard, entries []entry) string {
	found := false
	for _, e := range entries {
		if !overlaps(tools(e.Matcher), tools(g.Matcher)) {
			continue
		}
		for _, c := range e.Commands {
			if e.Matcher == g.Matcher && c == gd.Command {
				return Registered
			}
			if signature(c) {
				found = true
			}
		}
	}
	if found {
		return Different
	}
	return Missing
}

// pluginStatus judges the mdnav call in the installed opencode plugin's
// text: the plugin's branches are not parsed.
func pluginStatus(src string) string {
	switch {
	case strings.Contains(src, `"mdnav", "hook", "--agent", "opencode"`):
		return Registered
	case signature(src):
		return Different
	}
	return Missing
}

// CheckInstall reads agent's hook registration and reports the mdnav guard
// of each group as registered, missing or different from --print-install. It
// returns 0 when all are registered, 1 when one is not, 2 when the
// registration cannot be read.
func CheckInstall(w io.Writer, agent string, asJSON bool, getenv func(string) string) int {
	type row struct {
		Group   string `json:"group"`
		Matcher string `json:"matcher"`
		Guard   string `json:"guard"`
		Status  string `json:"status"`
		Want    string `json:"want"`
	}
	report := struct {
		Agent  string `json:"agent"`
		Config string `json:"config"`
		Guards []row  `json:"guards"`
		Result string `json:"result"`
		Error  string `json:"error,omitempty"`
	}{Agent: agent, Config: configPath(agent, getenv), Guards: []row{}}
	code := 0
	gs, err := groups(agent, getenv)
	var entries []entry
	var src string
	if err == nil {
		raw, rerr := os.ReadFile(report.Config)
		switch {
		case errors.Is(rerr, fs.ErrNotExist):
		case rerr != nil:
			err = rerr
		case agent == AgentClaude:
			entries, err = claudeEntries(raw)
		case agent == AgentCodex:
			entries, err = codexEntries(string(raw))
		default:
			src = string(raw)
		}
	}
	if err != nil {
		report.Result, report.Error, code = "cannot read", err.Error(), 2
	} else {
		for _, g := range gs {
			for _, gd := range g.Guards {
				var st string
				if agent == AgentOpencode {
					st = pluginStatus(src)
				} else {
					st = status(g, gd, entries)
				}
				if st != Registered {
					code = 1
				}
				report.Guards = append(report.Guards, row{g.Name, g.Matcher, gd.Name, st, gd.Command})
			}
		}
		report.Result = Registered
		if code == 1 {
			report.Result = "missing or different"
		}
	}
	if asJSON {
		json.MarshalWrite(w, report)
		fmt.Fprintln(w)
		return code
	}
	fmt.Fprintf(w, "mdnav hook --check-install --agent %s\nconfig: %s\n", agent, report.Config)
	if report.Error != "" {
		fmt.Fprintf(w, "cannot read: %s\n", report.Error)
		return code
	}
	for _, r := range report.Guards {
		fmt.Fprintf(w, "%-6s %-8s %s", r.Group, r.Guard, r.Status)
		if r.Status != Registered {
			fmt.Fprintf(w, "  (want %s: %s)", r.Matcher, r.Want)
		}
		fmt.Fprintln(w)
	}
	if agent == AgentOpencode {
		fmt.Fprintln(w, "note: the plugin's text is searched; which branch calls each guard is not checked")
	}
	if agent == AgentCodex {
		fmt.Fprintln(w, "note: whether codex trusts these hooks is not checked; start codex once interactively to trust them")
	}
	fmt.Fprintf(w, "result: %s\n", report.Result)
	return code
}

// claudeEntries reads only hooks.PreToolUse of a Claude settings file.
func claudeEntries(raw []byte) ([]entry, error) {
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	var es []entry
	for _, p := range s.Hooks.PreToolUse {
		e := entry{Matcher: p.Matcher}
		for _, h := range p.Hooks {
			if h.Type == "command" {
				e.Commands = append(e.Commands, h.Command)
			}
		}
		es = append(es, e)
	}
	return es, nil
}

// codexEntries reads only the [[hooks.PreToolUse]] and
// [[hooks.PreToolUse.hooks]] tables of a Codex config.toml: their matcher
// and command strings. Every other table is skipped unread.
func codexEntries(src string) ([]entry, error) {
	var es []entry
	in := "" // the table being read: "entry", "hook" or "" for any other
	for n, line := range strings.Split(src, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") {
			switch {
			case !strings.HasSuffix(l, "]"):
				return nil, fmt.Errorf("line %d: unclosed table header", n+1)
			case strings.HasPrefix(l, "[[") != strings.HasSuffix(l, "]]"):
				return nil, fmt.Errorf("line %d: mismatched table header", n+1)
			}
			switch strings.Join(strings.Fields(strings.Trim(l, "[]")), "") {
			case "hooks.PreToolUse":
				in = "entry"
				es = append(es, entry{})
			case "hooks.PreToolUse.hooks":
				if len(es) == 0 {
					return nil, fmt.Errorf("line %d: hooks.PreToolUse.hooks before hooks.PreToolUse", n+1)
				}
				in = "hook"
			default:
				in = ""
			}
			continue
		}
		if in == "" {
			continue
		}
		key, val, ok := strings.Cut(l, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: not key = value", n+1)
		}
		key = strings.TrimSpace(key)
		if key != "matcher" && key != "command" {
			continue
		}
		s, err := tomlString(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %v", n+1, key, err)
		}
		e := &es[len(es)-1]
		switch {
		case in == "entry" && key == "matcher":
			e.Matcher = s
		case in == "hook" && key == "command":
			e.Commands = append(e.Commands, s)
		}
	}
	return es, nil
}

// tomlString reads a one-line TOML basic or literal string.
func tomlString(v string) (string, error) {
	if i := strings.Index(v, " #"); i >= 0 && !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") {
		v = strings.TrimSpace(v[:i])
	}
	switch {
	case len(v) >= 2 && v[0] == '\'' && strings.LastIndexByte(v, '\'') > 0:
		return v[1:strings.LastIndexByte(v, '\'')], nil
	case len(v) >= 2 && v[0] == '"':
		end := 1
		for ; end < len(v); end++ {
			if v[end] == '\\' {
				end++
				continue
			}
			if v[end] == '"' {
				break
			}
		}
		if end >= len(v) {
			return "", errors.New("unclosed string")
		}
		return strconv.Unquote(v[:end+1])
	}
	return "", errors.New("not a string")
}

// opencodeStep1 and opencodeRest are the fixed parts of the opencode
// install text, around the MDNAV_CMD line.
const opencodeStep1 = "#\n# 1. Add to identuum-guard.js. In tool.execute.before, call mdnavGuard(input,\n#    output, directory) once per tool call, after the other hooks, in the bash\n#    branch and in the edit/write/multiedit/apply_patch branch. It sends\n#    opencode's own tool name and args, not the Claude-shaped payload the\n#    other hooks get, and reads the same Claude-shaped deny JSON they answer.\n"

const opencodeRest = "function mdnavGuard(input, output, directory) {\n  const call = JSON.stringify({tool: input.tool, args: output.args, cwd: directory, sessionID: input.sessionID});\n  const r = childProcess.spawnSync(MDNAV_CMD[0], MDNAV_CMD.slice(1), {\n    cwd: directory, input: call, encoding: 'utf8', timeout: 5000, maxBuffer: 1024 * 1024, shell: false,\n  });\n  if (r.error || r.signal || r.status !== 0 || /WARNING/i.test(r.stderr)) {\n    throw new Error(`identuum-guard: mdnav hook failed: ${r.error?.message ?? r.stderr}`);\n  }\n  const out = r.stdout.trim();\n  if (!out) return;\n  let d;\n  try { d = JSON.parse(out).hookSpecificOutput; } catch { throw new Error('identuum-guard: mdnav hook printed invalid JSON'); }\n  if (d?.permissionDecision === 'deny') throw new Error(`identuum-guard: ${d.permissionDecisionReason}`);\n  throw new Error('identuum-guard: mdnav hook printed an unknown result');\n}\n#\n# 2. Add to identuum-guard.test.mjs:\ntest('mdnav hook runs last, once, with opencode tool name and args', async t => {\n  const calls = [];\n  t.mock.method(childProcess, 'spawnSync', (bin, args, opts) => {\n    calls.push({argv: [bin, ...args], payload: JSON.parse(opts.input)});\n    return {status: 0, stdout: '', stderr: ''};\n  });\n  const guard = await IdentuumGuard({directory: workspace});\n  await guard['tool.execute.before']({tool: 'bash', sessionID: 'fixture'}, {args: {command: 'cat README.md', workdir: root}});\n  assert.deepEqual(calls.at(-1).argv.slice(-4), ['mdnav', 'hook', '--agent', 'opencode']);\n  assert.deepEqual(calls.at(-1).payload, {tool: 'bash', args: {command: 'cat README.md', workdir: root}, cwd: workspace, sessionID: 'fixture'});\n  calls.length = 0;\n  const patchText = '*** Begin Patch\\n*** Update File: a.md\\n*** Delete File: b.md\\n*** End Patch';\n  await guard['tool.execute.before']({tool: 'apply_patch', sessionID: 'fixture'}, {args: {patchText}});\n  const mdnav = calls.filter(c => c.argv.includes('mdnav'));\n  assert.equal(mdnav.length, 1);\n  assert.deepEqual(mdnav[0].payload.args, {patchText});\n});\n\ntest('mdnav denial keeps its reason', async t => {\n  t.mock.method(childProcess, 'spawnSync', bin => bin === 'env'\n    ? {status: 0, stdout: '{\"hookSpecificOutput\":{\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"use mdnav get\"}}', stderr: ''}\n    : {status: 0, stdout: '', stderr: ''});\n  await assert.rejects(invoke(shell('cat README.md')), /identuum-guard: use mdnav get/);\n});\n#\n# 3. In identuum-guard.test.mjs, the mdnav call changes two existing tests:\n#    'payload mapping, working directory and hook ordering': calls.length is 3\n#      for bash, calls[2] is mdnav; the write payload check stays calls[0].\n#    'patch maps every source, destination and deletion before allowing':\n#      map only calls that carry tool_input (calls.filter(p => p.tool_input)).\n#\n# 4. Install the plugin:\n"
