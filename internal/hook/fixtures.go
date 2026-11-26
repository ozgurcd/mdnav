package hook

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultFixtures is the shared fixture folder of mdnav, gograph and achta
// hooks: one folder per agent of recordings made with --record.
const DefaultFixtures = "/Users/odemir/Development/identuum/wiki/contracts/hook-fixtures"

// A fixture is <dir>/<agent>/<name>.json, a recording, with
// <dir>/<agent>/<name>.want holding one line "<verdict> <tool_kind>", such
// as "deny edit". It replays in a fresh copy of the case tree (Fixture):
// the recorded cwd is replaced by the copy, so record in a tree made with
// mdnav hook --write-fixture DIR.

// ReplayFixtures replays every fixture under dir, ignoring the fixture
// gate, and reports each on w. A recording without a .want file is listed
// and does not fail. It returns how many fixtures each agent has, and an
// error naming the fixtures that failed.
func ReplayFixtures(w io.Writer, dir string) (map[string]int, error) {
	counts := map[string]int{}
	var failed []string
	for _, agent := range Agents {
		files, _ := filepath.Glob(filepath.Join(dir, agent, "*.json"))
		sort.Strings(files)
		counts[agent] = len(files)
		for _, f := range files {
			name := agent + "/" + filepath.Base(f)
			status, err := replay(agent, f)
			if err != nil {
				failed = append(failed, name)
				fmt.Fprintf(w, "FAIL fixture %s: %v\n", name, err)
				continue
			}
			fmt.Fprintf(w, "%-4s fixture %s\n", status, name)
		}
	}
	if len(failed) > 0 {
		return counts, errors.New("hook fixtures failed: " + strings.Join(failed, ", "))
	}
	return counts, nil
}

// replay checks one fixture: the read layer reads it, its kind and verdict
// match the .want file. status is "ok" or "new" (no .want yet).
func replay(agent, file string) (status string, err error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	ev, err := read(agent, raw)
	if err != nil {
		return "", err
	}
	wantRaw, err := os.ReadFile(strings.TrimSuffix(file, ".json") + ".want")
	if errors.Is(err, os.ErrNotExist) {
		return "new", nil
	}
	if err != nil {
		return "", err
	}
	want := strings.Fields(string(wantRaw))
	if len(want) != 2 {
		return "", fmt.Errorf(".want holds %q, want \"<verdict> <tool_kind>\"", strings.TrimSpace(string(wantRaw)))
	}
	if ev.Kind != want[1] {
		return "", fmt.Errorf("tool kind %s, want %s", ev.Kind, want[1])
	}
	dir, err := os.MkdirTemp("", "mdnav-hook-fixture-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := WriteFixture(dir); err != nil {
		return "", err
	}
	rebase(&ev, dir)
	out := Evaluate(ev, false, time.Unix(0, 0))
	if out.Verdict != want[0] {
		return "", fmt.Errorf("verdict %s, want %s\n%s", out.Verdict, want[0], out.Reason)
	}
	return "ok", nil
}

// rebase moves an Event from its recorded cwd to dir.
func rebase(ev *Event, dir string) {
	old := ev.Cwd
	ev.Cwd = dir
	if old == "" {
		return
	}
	ev.Command = strings.ReplaceAll(ev.Command, old, dir)
	for i, p := range ev.Paths {
		ev.Paths[i] = strings.Replace(p, old, dir, 1)
	}
	for i, o := range ev.Patch {
		ev.Patch[i].Path = strings.Replace(o.Path, old, dir, 1)
		ev.Patch[i].To = strings.Replace(o.To, old, dir, 1)
	}
}
