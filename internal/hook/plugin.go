package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// textEdit replaces a run of whole lines that occurs exactly once. With
// prefix, the first old line matches any line that starts with it.
type textEdit struct {
	old, new []string
	prefix   bool
}

// applyEdits applies edits in order to src, a text of lines.
func applyEdits(src string, edits []textEdit) (string, error) {
	lines := splitLines(src)
	for _, e := range edits {
		var at []int
		for i := 0; i+len(e.old) <= len(lines); i++ {
			ok := true
			for j, o := range e.old {
				l := lines[i+j]
				if !(l == o || j == 0 && e.prefix && strings.HasPrefix(l, o)) {
					ok = false
					break
				}
			}
			if ok {
				at = append(at, i)
			}
		}
		switch len(at) {
		case 0:
			return "", fmt.Errorf("line %q not found", e.old[0])
		case 1:
		default:
			return "", fmt.Errorf("line %q found %d times", e.old[0], len(at))
		}
		i := at[0]
		lines = append(lines[:i:i], append(append([]string{}, e.new...), lines[i+len(e.old):]...)...)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// unifiedDiff returns the unified diff of a and b with three lines of
// context, "" when they are the same.
func unifiedDiff(name string, a, b []string) string {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte // ' ', '-' or '+'
		line string
		ai   int // lines of a before this op
		bi   int // lines of b before this op
	}
	var ops []op
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, op{' ', a[i], i, j})
			i, j = i+1, j+1
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', a[i], i, j})
			i++
		default:
			ops = append(ops, op{'+', b[j], i, j})
			j++
		}
	}
	const ctx = 3
	var out strings.Builder
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		start, end := max(0, k-ctx), k // end: one past the last change
		for e := k; e < len(ops); e++ {
			if ops[e].kind != ' ' {
				end = e + 1
			} else if e-end >= 2*ctx {
				break
			}
		}
		stop := min(len(ops), end+ctx)
		var body strings.Builder
		oldN, newN := 0, 0
		for _, o := range ops[start:stop] {
			body.WriteString(string(o.kind) + o.line + "\n")
			if o.kind != '+' {
				oldN++
			}
			if o.kind != '-' {
				newN++
			}
		}
		oldS, newS := ops[start].ai+1, ops[start].bi+1
		if oldN == 0 {
			oldS--
		}
		if newN == 0 {
			newS--
		}
		if out.Len() == 0 {
			out.WriteString("--- a/" + name + "\n+++ b/" + name + "\n")
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n%s", oldS, oldN, newS, newN, body.String())
		k = stop
	}
	return out.String()
}

// opencodeArgv is the mdnav argv line the plugin gets: with --record while
// opencode has no fixtures.
func opencodeArgv() []string {
	if fixtureBacked[AgentOpencode] {
		return []string{
			"// mdnav reads opencode's own tool calls.",
			"const MDNAV_ARGV = ['hook', '--agent', 'opencode'];",
		}
	}
	return []string{
		"// mdnav reads opencode's own tool calls. Until mdnav has opencode fixtures it",
		"// refuses them, so it records each call here and allows it.",
		"const MDNAV_RECORD = path.join(process.env.HOME || '/private/tmp', '.config/opencode/mdnav-record');",
		"const MDNAV_ARGV = ['hook', '--agent', 'opencode', '--record', MDNAV_RECORD];",
	}
}

// The mdnav calls the plugin makes, one per tool call.
const (
	opencodeShellCall = "        mdnav(tool, args, cwd, input.sessionID, workdir);"
	opencodeEditCall  = "      mdnav(tool, args, cwd, input.sessionID);"
)

// pluginEdits change identuum-guard.js from one Claude-shaped mdnav call
// per file to one opencode-shaped call per tool call.
func pluginEdits() []textEdit {
	return []textEdit{
		{old: []string{"const MDNAV_ENV = "}, prefix: true, new: nil}, // filled below
		{old: []string{"function patchPaths(text) {"}, new: []string{
			"// mdnav judges each tool call once, in opencode's own tool name and args.",
			"// Write content is dropped and a patch keeps only its *** lines, so the",
			"// payload stays under the hook input bound; edit text over it is dropped",
			"// too, and mdnav then names mdnav outline instead of exact edits.",
			"function mdnav(tool, args, cwd, sessionID, workdir = cwd) {",
			"  let mdArgs = args;",
			"  if (tool === 'write') mdArgs = {filePath: args.filePath};",
			"  else if (tool === 'apply_patch' || tool === 'patch') {",
			"    mdArgs = {patchText: args.patchText.replaceAll('\\r\\n', '\\n').split('\\n').filter(line => line.startsWith('***')).join('\\n')};",
			"  } else if (tool !== 'bash' && Buffer.byteLength(JSON.stringify(args)) > 60000) {",
			"    mdArgs = {filePath: args.filePath, edits: args.edits?.map(edit => ({filePath: edit.filePath}))};",
			"  }",
			"  hook('mdnav', MDNAV_ARGV, {tool, args: mdArgs, cwd, ...(sessionID ? {sessionID} : {})}, workdir, MDNAV_ENV);",
			"}",
			"",
			"function patchPaths(text) {",
		}},
		{old: []string{"        hook('mdnav', ['hook'], payload, workdir, MDNAV_ENV);"}, new: []string{opencodeShellCall}},
		{old: []string{"        hook('mdnav', ['hook'], payload, cwd, MDNAV_ENV);", "      }"}, new: []string{"      }", opencodeEditCall}},
	}
}

// testEdits update identuum-guard.test.mjs for the opencode-shaped call.
var testEdits = []textEdit{
	{old: []string{"    assert.equal(calls.length, 2);"}, new: []string{"    assert.equal(calls.length, 3);"}},
	{old: []string{"    assert.deepEqual(calls[1].args, ['hook', 'cd']);"}, new: []string{
		"    assert.deepEqual(calls[1].args, ['hook', 'cd']);",
		"    assert.deepEqual(calls[2].args.slice(0, 3), ['hook', '--agent', 'opencode']);",
		"    assert.deepEqual(calls[2].payload, {tool: 'bash', args: {command: 'cd /absolute; make check', workdir: root}, cwd: workspace, sessionID: 'fixture'});",
		"    assert.equal(calls[2].cwd, root);",
	}},
	{old: []string{
		"    assert.deepEqual(calls.map(p => p.tool_input.file_path), ['a', 'b', 'c', 'd'].map(p => path.join(workspace, `reports/${p}.txt`)));",
		"    assert.ok(calls.every(p => p.tool_name === 'MultiEdit'));",
	}, new: []string{
		"    const hooks = calls.filter(p => p.tool_input);",
		"    assert.deepEqual(hooks.map(p => p.tool_input.file_path), ['a', 'b', 'c', 'd'].map(p => path.join(workspace, `reports/${p}.txt`)));",
		"    assert.ok(hooks.every(p => p.tool_name === 'MultiEdit'));",
		"    assert.deepEqual(calls.filter(p => p.tool).map(p => p.args.patchText), ['*** Begin Patch\\n*** Update File: reports/a.txt\\n*** Move to: reports/b.txt\\n*** Delete File: reports/c.txt\\n*** Add File: reports/d.txt\\n*** End Patch']);",
	}},
	{old: []string{"  const neutral = 'identuum-guard: no journal context for this session';"}, new: []string{
		"  test('mdnav judges each call once in opencode form; its denial keeps the reason', async t => {",
		"    const calls = [];",
		"    t.mock.method(childProcess, 'spawnSync', (bin, args, opts) => {",
		"      calls.push({bin, args, payload: JSON.parse(opts.input), env: opts.env});",
		"      return bin === 'mdnav'",
		"        ? {status: 0, stdout: JSON.stringify({hookSpecificOutput: {permissionDecision: 'deny', permissionDecisionReason: 'use mdnav replace'}}), stderr: ''}",
		"        : {status: 0, stdout: '', stderr: ''};",
		"    });",
		"    const guard = await IdentuumGuard({directory: workspace});",
		"    await assert.rejects(guard['tool.execute.before']({tool: 'write', sessionID: 'fixture'}, {args: {filePath: 'reports/a.md', content: 'x'.repeat(70000)}}), /identuum-guard: use mdnav replace/);",
		"    const mdnav = calls.filter(c => c.bin === 'mdnav');",
		"    assert.equal(mdnav.length, 1);",
		"    assert.deepEqual(mdnav[0].args.slice(0, 3), ['hook', '--agent', 'opencode']);",
		"    assert.deepEqual(mdnav[0].payload, {tool: 'write', args: {filePath: 'reports/a.md'}, cwd: workspace, sessionID: 'fixture'});",
		"    assert.equal(mdnav[0].env.MDNAV_HOOK_MODE, 'warn');",
		"  });",
		"",
		"  const neutral = 'identuum-guard: no journal context for this session';",
	}},
}

// pluginDiffs reads the plugin and its test and returns their diffs, with
// paths relative to the wiki root (three levels above the plugin).
func pluginDiffs(plugin string) (wiki, diffs string, err error) {
	wiki = filepath.Dir(filepath.Dir(filepath.Dir(plugin)))
	test := strings.TrimSuffix(plugin, ".js") + ".test.mjs"
	for _, f := range []struct {
		path  string
		edits []textEdit
	}{{plugin, nil}, {test, testEdits}} {
		src, err := os.ReadFile(f.path)
		if err != nil {
			return wiki, "", err
		}
		edits := f.edits
		if f.path == plugin {
			edits = pluginEdits()
			env := ""
			for _, l := range splitLines(string(src)) {
				if strings.HasPrefix(l, edits[0].old[0]) {
					env = l
				}
			}
			edits[0].new = append([]string{env}, opencodeArgv()...)
		}
		out, err := applyEdits(string(src), edits)
		if err != nil {
			return wiki, "", fmt.Errorf("%s: %w; it is not the version this diff was written for", f.path, err)
		}
		rel, _ := filepath.Rel(wiki, f.path)
		diffs += unifiedDiff(filepath.ToSlash(rel), splitLines(string(src)), splitLines(out))
	}
	return wiki, diffs, nil
}
