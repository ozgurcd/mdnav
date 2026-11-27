package hook

import (
	"encoding/json/jsontext"
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
	return "env MDNAV_HOOK_MODE=warn MDNAV_CANNOT_LOG=" + LogPath(agent, getenv) + " " + word(mdnavBinary()) + " hook --agent " + agent
}

// mdnavBinary is the mdnav a registration runs: by default this binary's
// absolute path, so an older mdnav earlier on PATH is never the one called.
var mdnavBinary = executablePath

// executablePath keeps symlinks: Homebrew's /opt/homebrew/bin/mdnav links
// into a versioned Caskroom folder that the next upgrade removes.
func executablePath() string {
	if p, err := os.Executable(); err == nil {
		return brewLink(p)
	}
	return "mdnav"
}

// brewLink returns <prefix>/bin/<name> for a binary inside a Homebrew
// <prefix>/Caskroom or <prefix>/Cellar folder when that link resolves to the
// same file, and p otherwise.
func brewLink(p string) string {
	for _, dir := range []string{"/Caskroom/", "/Cellar/"} {
		i := strings.Index(p, dir)
		if i < 0 {
			continue
		}
		link := filepath.Join(p[:i], "bin", filepath.Base(p))
		a, err1 := filepath.EvalSymlinks(link)
		b, err2 := filepath.EvalSymlinks(p)
		if err1 == nil && err2 == nil && a == b {
			return link
		}
	}
	return p
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
		return []group{{"shell", "^Bash$", shell}, {"edit", "^apply_patch$", []guard{m}}}, nil
	case AgentAgy:
		return []group{{"shell", "run_command", shell}, {"edit", "edit_file|write_file|write_to_file|replace_file_content|multi_replace_file_content", []guard{m}}}, nil
	case AgentOpencode:
		plugin := guard{"mdnav", "the mdnav-guard plugin --print-install prints"}
		return []group{{"shell", "shell|bash", []guard{plugin}}, {"edit", "edit|write|multiedit|patch|apply_patch", []guard{plugin}}}, nil
	}
	return nil, fmt.Errorf("no registration for agent %q", agent)
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
		return filepath.Join(d, "opencode", "plugins", "mdnav-guard.js")
	case AgentAgy:
		return filepath.Join(h, ".gemini", "config", "hooks.json")
	}
	return ""
}

// PrintInstall prints agent's complete mdnav registration and returns the
// exit status: 0, or 2 for an agent that is not measured.
func PrintInstall(w io.Writer, agent string, getenv func(string) string) int {
	if agent == AgentOpencode {
		// The whole output is the plugin file, so it can be redirected into place.
		fmt.Fprint(w, opencodePlugin(getenv))
		return 0
	}
	fmt.Fprintf(w, "# mdnav hook --print-install --agent %s\n", agent)
	record := word(mdnavBinary()) + " hook --agent " + agent + " --record " + filepath.Join(stateDir(getenv), "record")
	gs, err := groups(agent, getenv)
	if err != nil {
		fmt.Fprintf(w, "# %v\n", err)
		return 2
	}
	unbacked := !fixtureBacked[agent]
	switch agent {
	case AgentClaude:
		fmt.Fprintf(w, "# File: %s (merge into \"hooks\"; keep every other key)\n", configPath(agent, getenv))
		fmt.Fprint(w, "{\n  \"hooks\": {\n    \"PreToolUse\": [\n")
		printGroups(w, gs, "      ")
		fmt.Fprint(w, "    ]\n  }\n}\n")
	case AgentCodex:
		cfg := configPath(agent, getenv)
		fmt.Fprintf(w, "# File: %s (or the same groups in %s; not both)\n", cfg, filepath.Join(filepath.Dir(cfg), "hooks.json"))
		fmt.Fprint(w, "# Codex sends Bash and apply_patch to PreToolUse; apply_patch carries the patch\n"+
			"# in tool_input.command.\n")
		if unbacked {
			fmt.Fprintf(w, "# mdnav refuses --agent codex (exit 2, which blocks the call in Codex) until\n"+
				"# codex has fixtures; until then use\n#   %s\n# as the mdnav command.\n", record)
		}
		for _, g := range gs {
			fmt.Fprintf(w, "\n[[hooks.PreToolUse]]\nmatcher = %s\n", jsonString(g.Matcher))
			for _, gd := range g.Guards {
				fmt.Fprintf(w, "\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = %s\n", jsonString(gd.Command))
			}
		}
		fmt.Fprint(w, "\n# Owner step: run /hooks in codex and trust these hooks. Codex skips new or\n"+
			"# changed hooks until they are trusted, so lanes would skip them.\n")
	case AgentAgy:
		fmt.Fprintf(w, "# File: %s (merge the \"mdnav\" entry; keep the others)\n", configPath(agent, getenv))
		if unbacked {
			fmt.Fprintf(w, "# mdnav refuses --agent agy (exit 2) until agy has fixtures, and the docs do not\n"+
				"# say what agy does on a non-zero exit; until then use\n#   %s\n# as the mdnav command.\n", record)
		}
		fmt.Fprint(w, "{\n  \"mdnav\": {\n    \"PreToolUse\": [\n")
		printGroups(w, gs, "      ")
		fmt.Fprint(w, "    ]\n  }\n}\n")
	}
	return 0
}

// printGroups prints matcher groups as JSON array elements at indent.
func printGroups(w io.Writer, gs []group, indent string) {
	for i, g := range gs {
		fmt.Fprintf(w, "%s{\n%s  \"matcher\": %s,\n%s  \"hooks\": [\n", indent, indent, jsonString(g.Matcher), indent)
		for j, gd := range g.Guards {
			fmt.Fprintf(w, "%s    {\"type\": \"command\", \"command\": %s}%s\n", indent, jsonString(gd.Command), comma(j, len(g.Guards)))
		}
		fmt.Fprintf(w, "%s  ]\n%s}%s\n", indent, indent, comma(i, len(gs)))
	}
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

// pluginStatus judges the installed mdnav-guard plugin: registered when it
// is what --print-install prints now.
func pluginStatus(src, want string) string {
	switch {
	case src == want:
		return Registered
	case src != "":
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
		case agent == AgentAgy:
			entries, err = agyEntries(raw)
		default:
			src = string(raw)
		}
	}
	if err == nil && agent == AgentCodex {
		// Codex loads hooks.json beside config.toml too.
		hooksJSON := filepath.Join(filepath.Dir(report.Config), "hooks.json")
		raw, rerr := os.ReadFile(hooksJSON)
		switch {
		case errors.Is(rerr, fs.ErrNotExist):
		case rerr != nil:
			err = rerr
		default:
			report.Config += " + " + hooksJSON
			var more []entry
			if more, err = claudeEntries(raw); err == nil {
				entries = append(entries, more...)
			}
		}
	}
	if err != nil {
		report.Result, report.Error, code = "cannot read", err.Error(), 2
	} else {
		for _, g := range gs {
			for _, gd := range g.Guards {
				var st string
				if agent == AgentOpencode {
					st = pluginStatus(src, opencodePlugin(getenv))
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
		fmt.Fprintln(w, "note: the plugin is compared with what --print-install prints now")
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

// agyEntries reads only the PreToolUse groups of the enabled named hooks
// of an Antigravity hooks.json.
func agyEntries(raw []byte) ([]entry, error) {
	var top map[string]jsontext.Value
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	type hook struct {
		Enabled    *bool `json:"enabled"`
		PreToolUse []struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"PreToolUse"`
	}
	var es []entry
	for _, v := range top {
		var h hook
		if v.Kind() != '{' || json.Unmarshal(v, &h) != nil {
			continue // not a named hook, such as "$schema"
		}
		if h.Enabled != nil && !*h.Enabled {
			continue
		}
		for _, p := range h.PreToolUse {
			e := entry{Matcher: p.Matcher}
			for _, hd := range p.Hooks {
				if hd.Type == "" || hd.Type == "command" {
					e.Commands = append(e.Commands, hd.Command)
				}
			}
			es = append(es, e)
		}
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
