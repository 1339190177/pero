//go:build windows

package nativetools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/runtime"
	"github.com/phodal/pero/harness/go/sandbox"
)

func TestWindowsPartialMountIsShellOnlyAndAlwaysAsks(t *testing.T) {
	if os.Getenv("PERO_REQUIRE_WINDOWS_ACL") != "1" {
		t.Skip("requires explicit disposable Windows ACL test authorization")
	}
	pwsh, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Fatal(err)
	}
	pwsh, err = filepath.Abs(pwsh)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	h, err := harness.Boot(t.Context(), harness.Options{ApprovalPolicy: harness.ApprovalAsk})
	if err != nil {
		t.Fatal(err)
	}
	app, err := Mount(t.Context(), h, Options{Workspace: root, EnableShell: true, Shell: pwsh, Sandbox: &sandbox.Config{Backend: sandbox.BackendWindowsACL, Mode: sandbox.WorkspaceWrite}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { h.Dispose(context.Background()); h.Drain(context.Background()); app.Close() }()
	if err := app.CheckCWD(root); err != nil {
		t.Fatal(err)
	}
	if err := app.CheckCWD(t.TempDir()); err == nil {
		t.Fatal("unrelated session cwd accepted")
	}
	asked := false
	a, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "windows-partial", CWD: root, Configure: func(ctx context.Context, run *runtime.Run, _ *harness.Agent) error {
		_, err := h.Approvals().Register(run, func(_ context.Context, request harness.ApprovalRequest) (harness.ApprovalOutcome, error) {
			asked = true
			if request.Native != nil {
				t.Error("partial sandbox advertised a strict native approval descriptor")
			}
			return harness.ApprovalRejected, nil
		})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := h.Tools().SchemasForAgent(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) != 1 || schemas[0].Name != "shell" || !strings.Contains(schemas[0].Description, "NOT isolated") {
		t.Fatalf("Windows native tools or boundary description: %+v", schemas)
	}
	a.Session().Append("turn/start", map[string]int{"turn": 1})
	args, err := json.Marshal(map[string]string{"command": "[IO.File]::WriteAllText('denied.txt', 'bad')"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: a, Name: "shell", CallID: "deny", Arguments: args})
	if err != nil || !out.Result.IsError || !asked {
		t.Fatalf("approval=%v out=%+v err=%v", asked, out, err)
	}
	if _, err := os.Stat(filepath.Join(root, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied shell executed: %v", err)
	}
}
