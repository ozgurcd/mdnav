package hook

import (
	"strings"
)

// decideTool judges an edit, multi_edit or write call. It returns what the
// call is, for the log.
func decideTool(ev Event) (decision, string) {
	if len(ev.Paths) > 1 {
		return decideEach(ev)
	}
	if len(ev.Paths) == 0 || !isMD(ev.Paths[0]) {
		return decision{}, ev.Tool
	}
	abs := resolve(ev.Cwd, ev.Paths[0])
	what := ev.Tool + " " + abs
	if !isFile(abs) {
		return decision{}, what // a new file, or one the tool cannot edit anyway
	}
	file := display(ev.Cwd, abs)
	if ev.Kind == KindWrite {
		return overwrite(file, ev.Tool), what
	}
	cmd, ok := subCommands(ev.Cwd, file, ev.Edits)
	if !ok {
		cmd = mdnav("outline", nil, file)
	}
	return editDecision(file, ev.Tool, cmd, !ok), what
}

// decideEach judges a multi-file edit one file at a time, with that file's
// edits, and keeps the highest rule.
func decideEach(ev Event) (decision, string) {
	var best decision
	var whats []string
	for _, p := range ev.Paths {
		one := ev
		one.Paths, one.Edits = []string{p}, nil
		for _, e := range ev.Edits {
			if e.Path == p {
				one.Edits = append(one.Edits, e)
			}
		}
		d, what := decideTool(one)
		whats = append(whats, what)
		if d.rule != "" && (best.rule == "" || rank(d.rule) > rank(best.rule)) {
			best = d
		}
	}
	return best, strings.Join(whats, "; ")
}

// decidePatch judges each file of a patch: adding over an existing
// Markdown file or moving onto one is an overwrite, updating one is an
// edit. Adding a new file and deleting one pass, as rm does.
func decidePatch(ev Event) (decision, string) {
	var best decision
	var whats []string
	take := func(d decision) {
		if best.rule == "" || rank(d.rule) > rank(best.rule) {
			best = d
		}
	}
	existing := func(p string) (string, bool) {
		abs := resolve(ev.Cwd, p)
		whats = append(whats, abs)
		return display(ev.Cwd, abs), isMD(p) && isFile(abs)
	}
	for _, o := range ev.Patch {
		file, ok := existing(o.Path)
		switch {
		case o.Op == "add" && ok:
			take(overwrite(file, ev.Tool))
		case o.Op == "update" && ok:
			take(editDecision(file, ev.Tool, mdnav("outline", nil, file), true))
		}
		if o.To != "" {
			if to, ok := existing(o.To); ok {
				take(overwrite(to, ev.Tool))
			}
		}
	}
	return best, ev.Tool + " " + strings.Join(whats, " ")
}

func overwrite(file, tool string) decision {
	cmd := mdnav("outline", nil, file)
	var b strings.Builder
	b.WriteString("mdnav hook: " + file + " exists; change its blocks with mdnav instead of rewriting it with " + tool + ".\n")
	b.WriteString("Run: " + cmd + "\n")
	b.WriteString("Then replace each block that changes: mdnav replace '<block>' --from NEW.txt --expect '<hash>' " + word(file) + "\n")
	b.WriteString(editTemplates(file))
	b.WriteString(toolEscapeHelp)
	return decision{rule: ruleOverwrite, reason: b.String(), command: cmd}
}

func editDecision(file, tool, cmd string, templates bool) decision {
	var b strings.Builder
	b.WriteString("mdnav hook: edit " + file + " with mdnav, not " + tool + ".\n")
	b.WriteString("Run: " + cmd + "\n")
	if templates {
		b.WriteString(editTemplates(file))
	}
	b.WriteString(toolEscapeHelp)
	return decision{rule: ruleEdit, reason: b.String(), command: cmd}
}

const toolEscapeHelp = "If mdnav cannot do this, make the change with a shell command that carries a comment saying why: # mdnav-cannot: <specific reason>"
