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
	wiki := fakeWiki(t)
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
# mdnav refuses --agent codex (exit 2, which blocks the call in Codex) until
# codex has fixtures; until then use
#   mdnav hook --agent codex --record /h/.local/state/mdnav/record
# as the mdnav command.

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
# mdnav refuses --agent agy (exit 2) until agy has fixtures, and the docs do not
# say what agy does on a non-zero exit; until then use
#   mdnav hook --agent agy --record /h/.local/state/mdnav/record
# as the mdnav command.
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
        "matcher": "write_to_file|replace_file_content|multi_replace_file_content",
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
			if r.want = strings.ReplaceAll(r.want, "WIKI", wiki); code != r.code || b.String() != r.want {
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
		{"opencode plugin with mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": strings.Join(opencodeArgv(), "\n") + "\n" + opencodeShellCall + "\n" + opencodeEditCall + "\n"}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered"}},
		{"opencode plugin without mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "run(`other guard`)"}, 1,
			[]string{"shell  mdnav    missing", "edit   mdnav    missing"}},
		{"opencode plugin with old mdnav", AgentOpencode, map[string]string{".config/opencode/plugins/identuum-guard.js": "        hook('mdnav', ['hook'], payload, workdir, MDNAV_ENV);\n"}, 1,
			[]string{"shell  mdnav    different", "edit   mdnav    different"}},
		{"opencode not installed", AgentOpencode, nil, 1, []string{"shell  mdnav    missing"}},
		{"codex hooks.json", AgentCodex, map[string]string{".codex/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":` + quote(mdnavFor("codex")) + `}]},{"matcher":"^apply_patch$","hooks":[{"type":"command","command":` + quote(mdnavFor("codex")) + `}]}]}}`}, 0,
			[]string{"shell  mdnav    registered", "edit   mdnav    registered"}},
		{"codex hooks.json unreadable", AgentCodex, map[string]string{".codex/hooks.json": `{`}, 2, []string{"cannot read"}},
		{"agy registered", AgentAgy, map[string]string{".gemini/config/hooks.json": `{"$schema":"x","other":{"PreToolUse":[]},"mdnav":{"PreToolUse":[{"matcher":"run_command","hooks":[{"command":` + quote(mdnavFor("agy")) + `}]},{"matcher":"write_to_file|replace_file_content|multi_replace_file_content","hooks":[{"command":` + quote(mdnavFor("agy")) + `}]}]}}`}, 0,
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

// fakePlugin and fakePluginTest hold every line the plugin edits anchor on,
// as identuum-guard.js and identuum-guard.test.mjs have them.
const fakePlugin = `const MDNAV_ENV = {MDNAV_HOOK_MODE: 'warn'};

function patchPaths(text) {
  return [];
}
      if (tool === 'bash') {
        hook('mdnav', ['hook'], payload, workdir, MDNAV_ENV);
        return;
      }
      for (const file of paths) {
        hook('bash', [WRAPPER], payload, cwd);
        hook('mdnav', ['hook'], payload, cwd, MDNAV_ENV);
      }
    },
`

const fakePluginTest = "    assert.equal(calls.length, 2);\n" +
	"    assert.deepEqual(calls[1].args, ['hook', 'cd']);\n" +
	"    assert.deepEqual(calls.map(p => p.tool_input.file_path), ['a', 'b', 'c', 'd'].map(p => path.join(workspace, `reports/${p}.txt`)));\n" +
	"    assert.ok(calls.every(p => p.tool_name === 'MultiEdit'));\n" +
	"  });\n" +
	"\n" +
	"  const neutral = 'identuum-guard: no journal context for this session';\n"

// fakeWiki writes the fake plugin and test under a wiki tree, points the
// install code at it, and returns the wiki root.
func fakeWiki(t *testing.T) string {
	wiki := filepath.Join(t.TempDir(), "wiki")
	dir := filepath.Join(wiki, "tools", "opencode")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "identuum-guard.js"), []byte(fakePlugin), 0o644)
	os.WriteFile(filepath.Join(dir, "identuum-guard.test.mjs"), []byte(fakePluginTest), 0o644)
	old := opencodePlugin
	opencodePlugin = filepath.Join(dir, "identuum-guard.js")
	t.Cleanup(func() { opencodePlugin = old })
	return wiki
}

// goldenOpencode is --print-install for opencode against fakeWiki; WIKI is
// its root.
const goldenOpencode = `# mdnav hook --print-install --agent opencode
# opencode runs no command hooks: the identuum-guard plugin calls mdnav.
# Plugin: WIKI/tools/opencode/identuum-guard.js
# The diff makes the plugin call mdnav once per tool call with opencode's own
# tool name and args (--agent opencode), and updates its tests to match.
# mdnav refuses --agent opencode until it has fixtures, so the plugin passes
# --record ~/.config/opencode/mdnav-record meanwhile: each call is recorded and
# allowed. Those recordings become opencode's fixtures.
#
# 1. Apply:   mdnav hook --print-install --agent opencode | git -C WIKI apply
# 2. Test:    (cd WIKI/tools/opencode && node --test identuum-guard.test.mjs)
# 3. Install: cp WIKI/tools/opencode/identuum-guard.js /h/.config/opencode/plugins/identuum-guard.js
--- a/tools/opencode/identuum-guard.js
+++ b/tools/opencode/identuum-guard.js
@@ -1,14 +1,33 @@
 const MDNAV_ENV = {MDNAV_HOOK_MODE: 'warn'};
+// mdnav reads opencode's own tool calls. Until mdnav has opencode fixtures it
+// refuses them, so it records each call here and allows it.
+const MDNAV_RECORD = path.join(process.env.HOME || '/private/tmp', '.config/opencode/mdnav-record');
+const MDNAV_ARGV = ['hook', '--agent', 'opencode', '--record', MDNAV_RECORD];
 
+// mdnav judges each tool call once, in opencode's own tool name and args.
+// Write content is dropped and a patch keeps only its *** lines, so the
+// payload stays under the hook input bound; edit text over it is dropped
+// too, and mdnav then names mdnav outline instead of exact edits.
+function mdnav(tool, args, cwd, sessionID, workdir = cwd) {
+  let mdArgs = args;
+  if (tool === 'write') mdArgs = {filePath: args.filePath};
+  else if (tool === 'apply_patch' || tool === 'patch') {
+    mdArgs = {patchText: args.patchText.replaceAll('\r\n', '\n').split('\n').filter(line => line.startsWith('***')).join('\n')};
+  } else if (tool !== 'bash' && Buffer.byteLength(JSON.stringify(args)) > 60000) {
+    mdArgs = {filePath: args.filePath, edits: args.edits?.map(edit => ({filePath: edit.filePath}))};
+  }
+  hook('mdnav', MDNAV_ARGV, {tool, args: mdArgs, cwd, ...(sessionID ? {sessionID} : {})}, workdir, MDNAV_ENV);
+}
+
 function patchPaths(text) {
   return [];
 }
       if (tool === 'bash') {
-        hook('mdnav', ['hook'], payload, workdir, MDNAV_ENV);
+        mdnav(tool, args, cwd, input.sessionID, workdir);
         return;
       }
       for (const file of paths) {
         hook('bash', [WRAPPER], payload, cwd);
-        hook('mdnav', ['hook'], payload, cwd, MDNAV_ENV);
       }
+      mdnav(tool, args, cwd, input.sessionID);
     },
--- a/tools/opencode/identuum-guard.test.mjs
+++ b/tools/opencode/identuum-guard.test.mjs
@@ -1,7 +1,29 @@
-    assert.equal(calls.length, 2);
+    assert.equal(calls.length, 3);
     assert.deepEqual(calls[1].args, ['hook', 'cd']);
-    assert.deepEqual(calls.map(p => p.tool_input.file_path), ['a', 'b', 'c', 'd'].map(p => path.join(workspace, ` + "`" + `reports/${p}.txt` + "`" + `)));
-    assert.ok(calls.every(p => p.tool_name === 'MultiEdit'));
+    assert.deepEqual(calls[2].args.slice(0, 3), ['hook', '--agent', 'opencode']);
+    assert.deepEqual(calls[2].payload, {tool: 'bash', args: {command: 'cd /absolute; make check', workdir: root}, cwd: workspace, sessionID: 'fixture'});
+    assert.equal(calls[2].cwd, root);
+    const hooks = calls.filter(p => p.tool_input);
+    assert.deepEqual(hooks.map(p => p.tool_input.file_path), ['a', 'b', 'c', 'd'].map(p => path.join(workspace, ` + "`" + `reports/${p}.txt` + "`" + `)));
+    assert.ok(hooks.every(p => p.tool_name === 'MultiEdit'));
+    assert.deepEqual(calls.filter(p => p.tool).map(p => p.args.patchText), ['*** Begin Patch\n*** Update File: reports/a.txt\n*** Move to: reports/b.txt\n*** Delete File: reports/c.txt\n*** Add File: reports/d.txt\n*** End Patch']);
   });
 
+  test('mdnav judges each call once in opencode form; its denial keeps the reason', async t => {
+    const calls = [];
+    t.mock.method(childProcess, 'spawnSync', (bin, args, opts) => {
+      calls.push({bin, args, payload: JSON.parse(opts.input), env: opts.env});
+      return bin === 'mdnav'
+        ? {status: 0, stdout: JSON.stringify({hookSpecificOutput: {permissionDecision: 'deny', permissionDecisionReason: 'use mdnav replace'}}), stderr: ''}
+        : {status: 0, stdout: '', stderr: ''};
+    });
+    const guard = await IdentuumGuard({directory: workspace});
+    await assert.rejects(guard['tool.execute.before']({tool: 'write', sessionID: 'fixture'}, {args: {filePath: 'reports/a.md', content: 'x'.repeat(70000)}}), /identuum-guard: use mdnav replace/);
+    const mdnav = calls.filter(c => c.bin === 'mdnav');
+    assert.equal(mdnav.length, 1);
+    assert.deepEqual(mdnav[0].args.slice(0, 3), ['hook', '--agent', 'opencode']);
+    assert.deepEqual(mdnav[0].payload, {tool: 'write', args: {filePath: 'reports/a.md'}, cwd: workspace, sessionID: 'fixture'});
+    assert.equal(mdnav[0].env.MDNAV_HOOK_MODE, 'warn');
+  });
+
   const neutral = 'identuum-guard: no journal context for this session';
`
