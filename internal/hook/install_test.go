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
# File: /h/.codex/config.toml
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
command = "` + mdnavFor("codex") + `"

# Owner step: start codex once interactively and trust the new hooks. Lanes run
# non-interactively and skip untrusted hooks.
`},
		{AgentOpencode, 0, `# mdnav hook --print-install --agent opencode
# opencode runs no command hooks: the guard plugin calls mdnav.
# Plugin: /Users/odemir/Development/identuum/wiki/tools/opencode/identuum-guard.js
# mdnav refuses --agent opencode (exit 2) until opencode has fixtures, and the
# call below denies on any non-zero exit, blocking every tool. Until then add
# "--record", "/h/.local/state/mdnav/record" to MDNAV_CMD.
#
# 1. Add to identuum-guard.js. In tool.execute.before, call mdnavGuard(input,
#    output, directory) once per tool call, after the other hooks, in the bash
#    branch and in the edit/write/multiedit/apply_patch branch. It sends
#    opencode's own tool name and args, not the Claude-shaped payload the
#    other hooks get, and reads the same Claude-shaped deny JSON they answer.
const MDNAV_CMD = ["env", "MDNAV_HOOK_MODE=warn", "MDNAV_CANNOT_LOG=/h/.local/state/mdnav/opencode-mdnav-cannot.log", "mdnav", "hook", "--agent", "opencode"];
function mdnavGuard(input, output, directory) {
  const call = JSON.stringify({tool: input.tool, args: output.args, cwd: directory, sessionID: input.sessionID});
  const r = childProcess.spawnSync(MDNAV_CMD[0], MDNAV_CMD.slice(1), {
    cwd: directory, input: call, encoding: 'utf8', timeout: 5000, maxBuffer: 1024 * 1024, shell: false,
  });
  if (r.error || r.signal || r.status !== 0 || /WARNING/i.test(r.stderr)) {
    throw new Error(` + "`identuum-guard: mdnav hook failed: ${r.error?.message ?? r.stderr}`" + `);
  }
  const out = r.stdout.trim();
  if (!out) return;
  let d;
  try { d = JSON.parse(out).hookSpecificOutput; } catch { throw new Error('identuum-guard: mdnav hook printed invalid JSON'); }
  if (d?.permissionDecision === 'deny') throw new Error(` + "`identuum-guard: ${d.permissionDecisionReason}`" + `);
  throw new Error('identuum-guard: mdnav hook printed an unknown result');
}
#
# 2. Add to identuum-guard.test.mjs:
test('mdnav hook runs last, once, with opencode tool name and args', async t => {
  const calls = [];
  t.mock.method(childProcess, 'spawnSync', (bin, args, opts) => {
    calls.push({argv: [bin, ...args], payload: JSON.parse(opts.input)});
    return {status: 0, stdout: '', stderr: ''};
  });
  const guard = await IdentuumGuard({directory: workspace});
  await guard['tool.execute.before']({tool: 'bash', sessionID: 'fixture'}, {args: {command: 'cat README.md', workdir: root}});
  assert.deepEqual(calls.at(-1).argv.slice(-4), ['mdnav', 'hook', '--agent', 'opencode']);
  assert.deepEqual(calls.at(-1).payload, {tool: 'bash', args: {command: 'cat README.md', workdir: root}, cwd: workspace, sessionID: 'fixture'});
  calls.length = 0;
  const patchText = '*** Begin Patch\n*** Update File: a.md\n*** Delete File: b.md\n*** End Patch';
  await guard['tool.execute.before']({tool: 'apply_patch', sessionID: 'fixture'}, {args: {patchText}});
  const mdnav = calls.filter(c => c.argv.includes('mdnav'));
  assert.equal(mdnav.length, 1);
  assert.deepEqual(mdnav[0].payload.args, {patchText});
});

test('mdnav denial keeps its reason', async t => {
  t.mock.method(childProcess, 'spawnSync', bin => bin === 'env'
    ? {status: 0, stdout: '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"use mdnav get"}}', stderr: ''}
    : {status: 0, stdout: '', stderr: ''});
  await assert.rejects(invoke(shell('cat README.md')), /identuum-guard: use mdnav get/);
});
#
# 3. In identuum-guard.test.mjs, the mdnav call changes two existing tests:
#    'payload mapping, working directory and hook ordering': calls.length is 3
#      for bash, calls[2] is mdnav; the write payload check stays calls[0].
#    'patch maps every source, destination and deletion before allowing':
#      map only calls that carry tool_input (calls.filter(p => p.tool_input)).
#
# 4. Install the plugin:
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
			[]string{"shell  mdnav    registered", "result: registered"}},
		{"codex today's mdnav", AgentCodex, map[string]string{".codex/config.toml": toml("mdnav hook")}, 1,
			[]string{"shell  mdnav    different"}},
		{"codex unreadable", AgentCodex, map[string]string{".codex/config.toml": "[[hooks.PreToolUse]\n"}, 2, []string{"cannot read"}},
		{"opencode plugin with mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "const MDNAV_CMD = [\"mdnav\", \"hook\", \"--agent\", \"opencode\"]"}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered"}},
		{"opencode plugin without mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "run(`other guard`)"}, 1,
			[]string{"shell  mdnav    missing", "edit   mdnav    missing"}},
		{"opencode plugin with old mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "$`mdnav hook`"}, 1,
			[]string{"shell  mdnav    different"}},
		{"opencode not installed", AgentOpencode, nil, 1, []string{"shell  mdnav    missing"}},
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
