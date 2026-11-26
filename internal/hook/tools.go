package hook

import (
	"encoding/json/v2"
	"strings"
)

type editSpec struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type toolInput struct {
	FilePath string `json:"file_path"`
	editSpec
	Edits []editSpec `json:"edits"`
}

// decideTool judges Edit, MultiEdit and Write. It returns what the call is,
// for the log.
func decideTool(in Input) (decision, string) {
	var ti toolInput
	if json.Unmarshal(in.ToolInput, &ti) != nil || ti.FilePath == "" || !isMD(ti.FilePath) {
		return decision{}, in.ToolName
	}
	abs := resolve(in.Cwd, ti.FilePath)
	what := in.ToolName + " " + abs
	if !isFile(abs) {
		return decision{}, what // a new file, or one the tool cannot edit anyway
	}
	file := display(in.Cwd, abs)
	var b strings.Builder
	var cmd string
	if in.ToolName == "Write" {
		cmd = mdnav("outline", nil, file)
		b.WriteString("mdnav hook: " + file + " exists; change its blocks with mdnav instead of rewriting it with Write.\n")
		b.WriteString("Run: " + cmd + "\n")
		b.WriteString("Then replace each block that changes: mdnav replace '<block>' --from NEW.txt --expect '<hash>' " + word(file) + "\n")
		b.WriteString(editTemplates(file))
		b.WriteString(toolEscapeHelp)
		return decision{rule: ruleOverwrite, reason: b.String(), command: cmd}, what
	}
	specs := ti.Edits
	if in.ToolName == "Edit" {
		specs = []editSpec{ti.editSpec}
	}
	pairs := make([]pair, len(specs))
	for i, s := range specs {
		pairs[i] = pair{Old: s.OldString, New: s.NewString, All: s.ReplaceAll}
	}
	cmd, ok := subCommands(in.Cwd, file, pairs)
	if !ok {
		cmd = mdnav("outline", nil, file)
	}
	b.WriteString("mdnav hook: edit " + file + " with mdnav, not " + in.ToolName + ".\n")
	b.WriteString("Run: " + cmd + "\n")
	if !ok {
		b.WriteString(editTemplates(file))
	}
	b.WriteString(toolEscapeHelp)
	return decision{rule: ruleEdit, reason: b.String(), command: cmd}, what
}

const toolEscapeHelp = "If mdnav cannot do this, make the change with a Bash command that carries a comment saying why: # mdnav-cannot: <specific reason>"
