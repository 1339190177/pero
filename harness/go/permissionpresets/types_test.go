package permissionpresets

import (
	"github.com/phodal/pero/harness/go/harness"
	"testing"
)

func TestOnlyExplicitAskingPresets(t *testing.T) {
	for _, id := range []string{"strict-read-only", "strict-workspace-write", "windows-acl-read-only", "windows-acl-workspace-write"} {
		p, e := Resolve(id)
		if e != nil || p.Approval != harness.ApprovalAsk {
			t.Fatal(p, e)
		}
	}
	for _, id := range []string{"", "auto", "danger-full-access", "workspace-write"} {
		if _, e := Resolve(id); e == nil {
			t.Fatal(id)
		}
	}
}
