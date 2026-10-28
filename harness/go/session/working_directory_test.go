package session

import "testing"

func TestWorkingDirectoryFollowsTheLatestChangeWithoutMovingTheHeader(t *testing.T) {
	session := NewSession("working-directory", "/original")
	if session.WorkingDirectory() != "/original" || session.Header().CWD != "/original" {
		t.Fatalf("directory=%q header=%q", session.WorkingDirectory(), session.Header().CWD)
	}
	if _, err := session.Append("working-directory/change", "nope"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("working-directory/change", map[string]string{"cwd": "  "}); err != nil {
		t.Fatal(err)
	}
	if session.WorkingDirectory() != "/original" {
		t.Fatalf("malformed events changed directory to %q", session.WorkingDirectory())
	}
	if _, err := session.Append("working-directory/change", map[string]string{"cwd": "/next"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("working-directory/change", map[string]int{"cwd": 1}); err != nil {
		t.Fatal(err)
	}
	if session.WorkingDirectory() != "/next" || session.Header().CWD != "/original" {
		t.Fatalf("directory=%q header=%q", session.WorkingDirectory(), session.Header().CWD)
	}
}
