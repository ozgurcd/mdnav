package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestBrewLink keeps registrations off Homebrew's versioned folders, which
// the next upgrade removes.
func TestBrewLink(t *testing.T) {
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "Caskroom", "mdnav", "0.1.3", "mdnav")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("x"), 0o755)
	if got := brewLink(bin); got != bin {
		t.Fatalf("no link: got %s, want %s", got, bin)
	}
	link := filepath.Join(prefix, "bin", "mdnav")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	if got := brewLink(bin); got != link {
		t.Fatalf("got %s, want %s", got, link)
	}
	other := filepath.Join(prefix, "go", "bin", "mdnav")
	if got := brewLink(other); got != other {
		t.Fatalf("non-Homebrew path changed: %s", got)
	}
}

// mdnavIn is the mdnav guard command an install gives agent in home.
func mdnavIn(home, agent string) string {
	return "env MDNAV_HOOK_MODE=warn MDNAV_CANNOT_LOG=" + home + "/.local/state/mdnav/" + agent + "-mdnav-cannot.log mdnav hook --agent " + agent
}

func mdnavFor(agent string) string { return mdnavIn("/h", agent) }

// TestPrintInstall is the --print-install table: the golden text per agent.
func TestPrintInstall(t *testing.T) {
	rows := []struct {
		agent string
		code  int
		want  string
	}{
		{AgentClaude, 0, `# mdnav hook --print-install --agent claude
# File: /h/.claude/settings.json (merge into "hooks"; keep every other key)
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "` + mdnavFor("claude") + `"}
        ]
      },
      {
        "matcher": "Edit|Write|MultiEdit",
        "hooks": [
          {"type": "command", "command": "` + mdnavFor("claude") + `"}
        ]
      }
    ]
  }
}
`},
		{AgentCodex, 0, `# mdnav hook --print-install --agent codex
# File: /h/.codex/config.toml (or the same groups in /h/.codex/hooks.json; not both)
# Codex sends Bash and apply_patch to PreToolUse; apply_patch carries the patch
# in tool_input.command.

[[hooks.PreToolUse]]
matcher = "^Bash$"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "` + mdnavFor("codex") + `"

[[hooks.PreToolUse]]
matcher = "^apply_patch$"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "` + mdnavFor("codex") + `"

# Owner step: run /hooks in codex and trust these hooks. Codex skips new or
# changed hooks until they are trusted, so lanes would skip them.
`},
		{AgentOpencode, 0, goldenOpencode},
		{AgentAgy, 0, `# mdnav hook --print-install --agent agy
# File: /h/.gemini/config/hooks.json (merge the "mdnav" entry; keep the others)
{
  "mdnav": {
    "PreToolUse": [
      {
        "matcher": "run_command",
        "hooks": [
          {"type": "command", "command": "` + mdnavFor("agy") + `"}
        ]
      },
      {
        "matcher": "edit_file|write_file|write_to_file|replace_file_content|multi_replace_file_content",
        "hooks": [
          {"type": "command", "command": "` + mdnavFor("agy") + `"}
        ]
      }
    ]
  }
}
`},
	}
	for _, r := range rows {
		t.Run(r.agent, func(t *testing.T) {
			var b bytes.Buffer
			code := PrintInstall(&b, r.agent, fakeEnv(map[string]string{"HOME": "/h"}))
			if code != r.code || b.String() != r.want {
				t.Fatalf("exit %d, want %d; diff:\n%s", code, r.code, lineDiff(r.want, b.String()))
			}
		})
	}
}

// TestCheckInstall is the --check-install table: registered, missing,
// different and unreadable per agent.
func TestCheckInstall(t *testing.T) {
	settings := func(shell, edit []string) string {
		hooks := func(cmds []string) string {
			var parts []string
			for _, c := range cmds {
				parts = append(parts, `{"type":"command","command":`+quote(c)+`}`)
			}
			return strings.Join(parts, ",")
		}
		return `{"model":"x","env":{"TOKEN":"never-printed"},"hooks":{"PreToolUse":[` +
			`{"matcher":"Bash","hooks":[` + hooks(shell) + `]},` +
			`{"matcher":"Edit|Write|MultiEdit","hooks":[` + hooks(edit) + `]}]}}`
	}
	toml := func(cmds ...string) string {
		s := "model = \"o3\"\n"
		for _, m := range []string{"^Bash$", "^apply_patch$"} {
			s += "\n[[hooks.PreToolUse]]\nmatcher = " + quote(m) + "\n"
			for _, c := range cmds {
				s += "\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = " + quote(c) + "\n"
			}
		}
		return s + "\n[profiles.x]\ncommand = \"mdnav hook\"\n"
	}
	home := t.TempDir()
	mdnavFor := func(agent string) string { return mdnavIn(home, agent) }
	all := []string{mdnavFor("claude")}
	rows := []struct {
		name, agent string
		files       map[string]string
		code        int
		want        []string // lines the report must hold
	}{
		{"claude registered", AgentClaude, map[string]string{".claude/settings.json": settings(all, []string{mdnavFor("claude")})}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered", "result: registered"}},
		{"claude today's mdnav", AgentClaude, map[string]string{".claude/settings.json": settings([]string{"mdnav hook"}, []string{"mdnav hook"})}, 1,
			[]string{"shell  mdnav    different", "edit   mdnav    different", "result: missing or different"}},
		{"claude bash only", AgentClaude, map[string]string{".claude/settings.json": settings([]string{mdnavFor("claude")}, nil)}, 1,
			[]string{"shell  mdnav    registered", "edit   mdnav    missing"}},
		{"claude no settings", AgentClaude, nil, 1, []string{"shell  mdnav    missing", "edit   mdnav    missing"}},
		{"claude unreadable", AgentClaude, map[string]string{".claude/settings.json": `{"hooks":`}, 2, []string{"cannot read"}},
		{"codex registered", AgentCodex, map[string]string{".codex/config.toml": toml(mdnavFor("codex"))}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered", "result: registered"}},
		{"codex today's mdnav", AgentCodex, map[string]string{".codex/config.toml": toml("mdnav hook")}, 1,
			[]string{"shell  mdnav    different"}},
		{"codex unreadable", AgentCodex, map[string]string{".codex/config.toml": "[[hooks.PreToolUse]\n"}, 2, []string{"cannot read"}},
		{"opencode plugin installed", AgentOpencode, map[string]string{".config/opencode/plugins/mdnav-guard.js": strings.ReplaceAll(goldenOpencode, "/h/", home+"/")}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered"}},
		{"opencode plugin changed", AgentOpencode, map[string]string{".config/opencode/plugins/mdnav-guard.js": "// mdnav-guard: an older copy\n"}, 1,
			[]string{"shell  mdnav    different", "edit   mdnav    different"}},
		{"opencode not installed", AgentOpencode, nil, 1, []string{"shell  mdnav    missing"}},
		{"codex hooks.json", AgentCodex, map[string]string{".codex/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":` + quote(mdnavFor("codex")) + `}]},{"matcher":"^apply_patch$","hooks":[{"type":"command","command":` + quote(mdnavFor("codex")) + `}]}]}}`}, 0,
			[]string{"config.toml + " + home + "/.codex/hooks.json", "shell  mdnav    registered", "edit   mdnav    registered"}},
		{"codex hooks.json unreadable", AgentCodex, map[string]string{".codex/hooks.json": `{`}, 2, []string{"cannot read"}},
		{"agy registered", AgentAgy, map[string]string{".gemini/config/hooks.json": `{"$schema":"x","other":{"PreToolUse":[]},"mdnav":{"PreToolUse":[{"matcher":"run_command","hooks":[{"command":` + quote(mdnavFor("agy")) + `}]},{"matcher":"edit_file|write_file|write_to_file|replace_file_content|multi_replace_file_content","hooks":[{"command":` + quote(mdnavFor("agy")) + `}]}]}}`}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered"}},
		{"agy disabled", AgentAgy, map[string]string{".gemini/config/hooks.json": `{"mdnav":{"enabled":false,"PreToolUse":[{"matcher":"run_command","hooks":[{"command":` + quote(mdnavFor("agy")) + `}]}]}}`}, 1,
			[]string{"shell  mdnav    missing"}},
		{"agy not installed", AgentAgy, nil, 1, []string{"shell  mdnav    missing", "edit   mdnav    missing"}},
		{"agy unreadable", AgentAgy, map[string]string{".gemini/config/hooks.json": `[`}, 2, []string{"cannot read"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			os.RemoveAll(home)
			for name, body := range r.files {
				p := filepath.Join(home, name)
				os.MkdirAll(filepath.Dir(p), 0o700)
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, asJSON := range []bool{false, true} {
				var b bytes.Buffer
				code := CheckInstall(&b, r.agent, asJSON, fakeEnv(map[string]string{"HOME": home}))
				out := b.String()
				if code != r.code {
					t.Fatalf("json %v: exit %d, want %d\n%s", asJSON, code, r.code, out)
				}
				if strings.Contains(out, "never-printed") || strings.Contains(out, `"o3"`) {
					t.Fatalf("report prints a settings value:\n%s", out)
				}
				if asJSON {
					if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"agent":"`+r.agent+`"`) {
						t.Fatalf("json report:\n%s", out)
					}
					continue
				}
				for _, w := range r.want {
					if !strings.Contains(out, w) {
						t.Fatalf("report lacks %q:\n%s", w, out)
					}
				}
			}
		})
	}
}

func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// lineDiff shows the first line where want and got part.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return "line " + strconv.Itoa(i+1) + "\nwant: " + a + "\ngot:  " + b
		}
	}
	return "(same)"
}

// goldenOpencode is the mdnav-guard plugin --print-install writes for
// opencode 2, with HOME=/h.
const goldenOpencode = `// mdnav-guard: makes opencode agents use mdnav for Markdown.
// Written by: mdnav hook --print-install --agent opencode
// Install:    mdnav hook --print-install --agent opencode > /h/.config/opencode/plugins/mdnav-guard.js
// Check:      mdnav hook --check-install --agent opencode
import { spawnSync } from "node:child_process";

const MDNAV = "mdnav";
const ARGV = ["hook", "--agent", "opencode"];
const ENV = {MDNAV_HOOK_MODE: "warn", MDNAV_CANNOT_LOG: "/h/.local/state/mdnav/opencode-mdnav-cannot.log"};
// The tools mdnav judges; every other tool passes without a call.
const TOOLS = new Set(["shell", "bash", "edit", "write", "multiedit", "patch", "apply_patch"]);

// mdnav needs paths, edits and patch headers, not file content.
function slim(tool, input) {
  if (tool === "write") return {path: input?.path, filePath: input?.filePath};
  if ((tool === "patch" || tool === "apply_patch") && typeof input?.patchText === "string") {
    return {patchText: input.patchText.split(/\r?\n/).filter(line => line.startsWith("***")).join("\n")};
  }
  return input;
}

export default {
  id: "mdnav.guard",
  async setup(ctx) {
    await ctx.tool.hook("execute.before", (event) => {
      if (!TOOLS.has(event.tool)) return;
      const cwd = ctx.location.directory;
      const call = {tool: event.tool, args: slim(event.tool, event.input), cwd, sessionID: event.sessionID};
      const r = spawnSync(MDNAV, ARGV, {
        cwd, input: JSON.stringify(call), encoding: "utf8",
        env: {...process.env, ...ENV}, timeout: 5000, maxBuffer: 1024 * 1024,
      });
      if (r.error || r.signal || r.status !== 0) {
        throw new Error("mdnav-guard: mdnav hook failed: " + String(r.error?.message || r.stderr || "").trim());
      }
      const out = String(r.stdout || "").trim();
      if (!out) return;
      let decision;
      try { decision = JSON.parse(out).hookSpecificOutput; } catch { throw new Error("mdnav-guard: mdnav hook printed invalid JSON"); }
      if (decision?.permissionDecision === "deny") throw new Error(decision.permissionDecisionReason);
      throw new Error("mdnav-guard: mdnav hook printed an unknown result");
    });
  },
};
`

// TestMain pins the mdnav binary the registrations name, so the goldens do
// not depend on where the test binary was built.
func TestMain(m *testing.M) {
	mdnavBinary = func() string { return "mdnav" }
	os.Exit(m.Run())
}

// TestBinaryPath checks that registrations name the binary that printed
// them by its absolute path, so an older mdnav earlier on PATH is not run.
func TestBinaryPath(t *testing.T) {
	old := mdnavBinary
	mdnavBinary = executablePath
	defer func() { mdnavBinary = old }()
	exe, _ := os.Executable()
	env := fakeEnv(map[string]string{"HOME": "/h"})
	if got := mdnavGuardCommand(AgentClaude, env); !filepath.IsAbs(exe) || !strings.Contains(got, " "+exe+" hook --agent claude") {
		t.Fatalf("command %q does not name %q", got, exe)
	}
	if got := opencodePlugin(env); !strings.Contains(got, "const MDNAV = "+jsonString(exe)+";") {
		t.Fatalf("plugin does not name %q", exe)
	}
}
