// Package permissionpresets adapts Point's independent sandbox/approval bundle.
// No UI, mutable session selectors, full-access or auto-approval presets.
package permissionpresets

import (
	"fmt"
	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/sandbox"
)

// Preset retains independent controls; confinement never implies approval.
type Preset struct {
	ID       string
	Sandbox  sandbox.Mode
	Approval harness.ApprovalPolicy
}

func Resolve(id string) (Preset, error) {
	switch id {
	case "strict-read-only":
		return Preset{ID: id, Sandbox: sandbox.ReadOnly, Approval: harness.ApprovalAsk}, nil
	case "strict-workspace-write":
		return Preset{ID: id, Sandbox: sandbox.WorkspaceWrite, Approval: harness.ApprovalAsk}, nil
	default:
		return Preset{}, fmt.Errorf("unsupported headless permission preset %q", id)
	}
}
