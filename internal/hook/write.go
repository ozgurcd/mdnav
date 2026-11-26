package hook

import (
	"encoding/json/v2"
	"fmt"
	"io"
)

// Write answers one judged call in agent's format and returns the exit
// status. Only a denial writes: an allowed call exits 0 with no output.
func Write(agent string, out Outcome, w io.Writer) (int, error) {
	if out.Verdict != Deny {
		return 0, nil
	}
	var resp any
	switch agent {
	case AgentClaude, AgentCodex:
		resp = map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": out.Reason,
		}}
	case AgentOpencode:
		resp = map[string]string{"decision": "deny", "reason": out.Reason}
	default:
		return 2, fmt.Errorf("%w for %s", ErrNoFixtures, agent)
	}
	if err := json.MarshalWrite(w, resp, json.Deterministic(true)); err != nil {
		return 1, err
	}
	_, err := fmt.Fprintln(w)
	return 0, err
}
