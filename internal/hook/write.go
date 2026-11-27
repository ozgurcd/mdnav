package hook

import (
	"encoding/json/v2"
	"fmt"
	"io"
)

// Write answers one judged call in agent's format and returns the exit
// status. For claude, codex and opencode only a denial writes; agy always
// gets a decision.
func Write(agent string, out Outcome, w io.Writer) (int, error) {
	if agent == AgentAgy {
		// Antigravity requires a decision; "allow" passes the call on to the
		// next hook or the default policy.
		resp := map[string]string{"decision": "allow"}
		if out.Verdict == Deny {
			resp = map[string]string{"decision": "deny", "reason": out.Reason}
		}
		return emit(w, resp)
	}
	if out.Verdict != Deny {
		return 0, nil
	}
	var resp any
	switch agent {
	case AgentClaude, AgentCodex, AgentOpencode:
		// The opencode guard plugin reads the same deny JSON as Claude.
		resp = map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": out.Reason,
		}}
	default:
		return 2, fmt.Errorf("%w for %s", ErrNoFixtures, agent)
	}
	return emit(w, resp)
}

func emit(w io.Writer, resp any) (int, error) {
	if err := json.MarshalWrite(w, resp, json.Deterministic(true)); err != nil {
		return 1, err
	}
	_, err := fmt.Fprintln(w)
	return 0, err
}
