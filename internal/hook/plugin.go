package hook

import (
	"path/filepath"
	"strings"
)

// opencodePlugin returns mdnav-guard, the opencode 2 plugin that calls
// mdnav hook --agent opencode from its tool execute.before hook. opencode
// loads .js files in ~/.config/opencode/plugins/ directly; Plugin.define is
// the identity, so the plain default export needs no package. While opencode
// has no fixtures the plugin passes --record.
func opencodePlugin(getenv func(string) string) string {
	record := filepath.Join(stateDir(getenv), "record")
	argv := `"hook", "--agent", "opencode"`
	var b strings.Builder
	b.WriteString("// mdnav-guard: makes opencode agents use mdnav for Markdown.\n" +
		"// Written by: mdnav hook --print-install --agent opencode\n" +
		"// Install:    mdnav hook --print-install --agent opencode > " + configPath(AgentOpencode, getenv) + "\n" +
		"// Check:      mdnav hook --check-install --agent opencode\n")
	if !fixtureBacked[AgentOpencode] {
		argv += `, "--record", ` + jsonString(record)
		b.WriteString("// mdnav has no opencode fixtures yet, so it records each call in\n" +
			"// " + record + " and allows it. Those recordings become the\n" +
			"// fixtures; then install this file again.\n")
	}
	b.WriteString(`import { spawnSync } from "node:child_process";

const ARGV = [` + argv + `];
const ENV = {MDNAV_HOOK_MODE: "warn", MDNAV_CANNOT_LOG: ` + jsonString(LogPath(AgentOpencode, getenv)) + `};
` + opencodePluginBody)
	return b.String()
}

const opencodePluginBody = `// The tools mdnav judges; every other tool passes without a call.
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
      const r = spawnSync("mdnav", ARGV, {
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
