package session

import "testing"

func appendTurn(t *testing.T, s *Session, turn int, text string) {
	t.Helper()
	mustAppend(t, s, "turn/start", map[string]int{"turn": turn})
	mustAppend(t, s, "user/message", Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: text}}})
	mustAppend(t, s, "turn/end", map[string]any{"turn": turn, "reason": "stop"})
}

func inboxFixtureMessage(id, text string) Message {
	return Message{ID: id, Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: text}}, Source: []byte(`{"kind":"user"}`)}
}
