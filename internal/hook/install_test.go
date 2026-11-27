package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
# Guards, in order: gograph, achta hook cd (shell only), mdnav
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "/Users/odemir/Development/identuum/wiki/tools/gograph-first-hook-wrapper.sh"},
          {"type": "command", "command": "achta hook cd"},
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
# File: /h/.codex/config.toml
# Guards, in order: gograph, achta hook cd (shell only), mdnav
# apply_patch: not measured whether Codex sends it to hooks, so there is no
# apply_patch matcher until a fixture shows it.
# Check the keys against the Codex hooks docs; they were not reachable when
# this text was written.
# mdnav refuses --agent codex (exit 2) until codex has fixtures; until then use
#   mdnav hook --agent codex --record /h/.local/state/mdnav/record
# as the mdnav command.
[features]
codex_hooks = true

[[hooks.PreToolUse]]
matcher = "^Bash$"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "/Users/odemir/Development/identuum/wiki/tools/gograph-first-hook-wrapper.sh"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "achta hook cd"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "` + mdnavFor("codex") + `"

# Owner step: start codex once interactively and trust the new hooks. Lanes run
# non-interactively and skip untrusted hooks.
`},
		{AgentOpencode, 0, `# mdnav hook --print-install --agent opencode
# opencode runs no command hooks: the guard plugin calls each guard.
# Plugin: /Users/odemir/Development/identuum/wiki/tools/opencode/identuum-guard.js
# Guards, in order: gograph, achta hook cd (shell only), mdnav
# mdnav refuses --agent opencode (exit 2) until opencode has fixtures, and the
# call below throws on any non-zero exit, blocking every tool. Until then add
# --record /h/.local/state/mdnav/record to MDNAV_CMD.
#
# 1. Add to identuum-guard.js, and call it in tool.execute.before after the
#    gograph and achta calls of the bash branch, and in the edit/write branch:
const MDNAV_CMD = ["env", "MDNAV_HOOK_MODE=warn", "MDNAV_CANNOT_LOG=/h/.local/state/mdnav/opencode-mdnav-cannot.log", "mdnav", "hook", "--agent", "opencode"]
async function mdnavGuard($, input, output, directory) {
  const call = JSON.stringify({tool: input.tool, args: output.args, cwd: directory, sessionID: input.sessionID})
  const r = await $` + "`${MDNAV_CMD} < ${new Response(call)}`" + `.nothrow().quiet()
  if (r.exitCode !== 0) throw new Error("mdnav hook: " + r.stderr.toString())
  const out = r.stdout.toString().trim()
  if (out && JSON.parse(out).decision === "deny") throw new Error(JSON.parse(out).reason)
}
#
# 2. Add to identuum-guard.test.mjs:
test("mdnav hook --agent opencode denies cat of a Markdown file", () => {
  const dir = mkdtempSync(join(tmpdir(), "mdnav-guard-"))
  writeFileSync(join(dir, "a.md"), "# A\n")
  const call = JSON.stringify({tool: "bash", args: {command: "cat a.md"}, cwd: dir})
  const out = execFileSync("mdnav", ["hook", "--agent", "opencode"], {input: call, env: {...process.env, MDNAV_HOOK_MODE: "deny"}}).toString()
  assert.equal(JSON.parse(out).decision, "deny")
})
#
# 3. Install the plugin:
cp /Users/odemir/Development/identuum/wiki/tools/opencode/identuum-guard.js /h/.config/opencode/plugins/identuum-guard.js
`},
		{AgentAgy, 2, `# mdnav hook --print-install --agent agy
# agy is not measured: its hooks.json path, edit tool names and decision words
# come from fixtures. Record first: register
#   mdnav hook --agent agy --record /h/.local/state/mdnav/record
# as agy's PreToolUse command, run one session, and add the recordings to
# <fixtures>/agy/.
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
	gograph := GographCommand
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
		s := "model = \"o3\"\n[features]\ncodex_hooks = true\n\n[[hooks.PreToolUse]]\nmatcher = \"^Bash$\"\n"
		for _, c := range cmds {
			s += "\n[[hooks.PreToolUse.hooks]]\ntype = \"command\"\ncommand = " + quote(c) + "\n"
		}
		return s + "\n[profiles.x]\ncommand = \"mdnav hook\"\n"
	}
	home := t.TempDir()
	mdnavFor := func(agent string) string { return mdnavIn(home, agent) }
	all := []string{gograph, AchtaCommand, mdnavFor("claude")}
	rows := []struct {
		name, agent string
		files       map[string]string
		code        int
		want        []string // lines the report must hold
	}{
		{"claude registered", AgentClaude, map[string]string{".claude/settings.json": settings(all, []string{mdnavFor("claude")})}, 0,
			[]string{"shell  gograph  registered", "shell  achta    registered", "shell  mdnav    registered", "edit   mdnav    registered", "result: registered"}},
		{"claude today's mdnav", AgentClaude, map[string]string{".claude/settings.json": settings([]string{gograph, AchtaCommand, "mdnav hook"}, []string{"mdnav hook"})}, 1,
			[]string{"shell  mdnav    different", "edit   mdnav    different", "result: missing or different"}},
		{"claude no edit group", AgentClaude, map[string]string{".claude/settings.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"achta hook cd"}]}]}}`}, 1,
			[]string{"shell  gograph  missing", "shell  achta    registered", "edit   mdnav    missing"}},
		{"claude no settings", AgentClaude, nil, 1, []string{"shell  gograph  missing", "edit   mdnav    missing"}},
		{"claude unreadable", AgentClaude, map[string]string{".claude/settings.json": `{"hooks":`}, 2, []string{"cannot read"}},
		{"codex registered", AgentCodex, map[string]string{".codex/config.toml": toml(gograph, AchtaCommand, mdnavFor("codex"))}, 0,
			[]string{"shell  gograph  registered", "shell  mdnav    registered", "result: registered"}},
		{"codex mdnav only", AgentCodex, map[string]string{".codex/config.toml": toml("mdnav hook")}, 1,
			[]string{"shell  gograph  missing", "shell  achta    missing", "shell  mdnav    different"}},
		{"codex unreadable", AgentCodex, map[string]string{".codex/config.toml": "[[hooks.PreToolUse]\n"}, 2, []string{"cannot read"}},
		{"opencode plugin with mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "run(`" + gograph + "`); run(`achta hook cd`); const MDNAV_CMD = [\"mdnav\", \"hook\", \"--agent\", \"opencode\"]"}, 0,
			[]string{"shell  gograph  registered", "edit   mdnav    registered"}},
		{"opencode plugin without mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "run(`" + gograph + "`); run(`achta hook cd`)"}, 1,
			[]string{"shell  achta    registered", "shell  mdnav    missing", "edit   mdnav    missing"}},
		{"opencode not installed", AgentOpencode, nil, 1, []string{"shell  gograph  missing"}},
		{"agy unmeasured", AgentAgy, nil, 2, []string{"cannot read: agy is not measured"}},
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
