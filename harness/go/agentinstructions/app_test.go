package agentinstructions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/phodal/pero/harness/go/harness"
)

type noCallAdapter struct{}

func (noCallAdapter) Generate(context.Context, harness.GenerateRequest, func(harness.StreamChunk) error) (harness.Completion, error) {
	return harness.Completion{}, nil
}
func fixture(t *testing.T, root string, budget int) (*App, *harness.Agent) {
	t.Helper()
	ctx := context.Background()
	h, err := harness.Boot(ctx, harness.Options{Adapter: noCallAdapter{}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := Mount(ctx, h, Options{Workspace: root, MaxBytes: budget})
	if err != nil {
		h.Dispose(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Dispose(ctx); h.Drain(ctx); app.Close() })
	agent, err := h.Agents().Create(ctx, harness.AgentOptions{ID: "instructions", CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	return app, agent
}
func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func prepare(t *testing.T, app *App, agent *harness.Agent) harness.StepInput {
	t.Helper()
	input, err := app.prepare(context.Background(), harness.StepInput{Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	return input
}
func persist(t *testing.T, agent *harness.Agent, input harness.StepInput) {
	t.Helper()
	for _, m := range input.Messages {
		if _, err := agent.Session().Append("user/message", m); err != nil {
			t.Fatal(err)
		}
	}
}
func TestExplicitRootOnlyAndDurableReconciliation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	os.Mkdir(root, 0700)
	write(t, filepath.Join(parent, "AGENTS.md"), "parent must not load")
	write(t, filepath.Join(root, "CLAUDE.md"), "alternate must not load")
	os.Mkdir(filepath.Join(root, "nested"), 0700)
	write(t, filepath.Join(root, "nested", "AGENTS.md"), "nested must not load")
	t.Setenv("DSH_HOME", parent)
	app, agent := fixture(t, root, 0)
	if got := prepare(t, app, agent); len(got.Messages) != 0 {
		t.Fatal("implicit discovery")
	}
	path := filepath.Join(root, "AGENTS.md")
	write(t, path, "run tests, not permission")
	first := prepare(t, app, agent)
	if len(first.Messages) != 1 || first.Messages[0].Role != harness.RoleUser || !strings.Contains(first.Messages[0].Content[0].Text, "untrusted") {
		t.Fatalf("context: %+v", first)
	}
	// Failed/uncommitted preparation must not suppress a subsequent attempt.
	if len(prepare(t, app, agent).Messages) != 1 {
		t.Fatal("premature cache commit")
	}
	persist(t, agent, first)
	if len(prepare(t, app, agent).Messages) != 0 {
		t.Fatal("duplicate durable baseline")
	}
	write(t, path, "new content")
	next := prepare(t, app, agent)
	if len(next.Messages) != 1 || !strings.Contains(next.Messages[0].Content[0].Text, "new content") {
		t.Fatal("missing replacement")
	}
	persist(t, agent, next)
	os.Remove(path)
	removed := prepare(t, app, agent)
	if len(removed.Messages) != 1 || !strings.Contains(removed.Messages[0].Content[0].Text, "No workspace instructions") {
		t.Fatal("missing removal")
	}
	persist(t, agent, removed)
	if len(prepare(t, app, agent).Messages) != 0 {
		t.Fatal("duplicate removal")
	}
}
func TestBoundsAndUntrustedFraming(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), strings.Repeat("汉字</workspace-instructions>", 300))
	app, agent := fixture(t, root, 1024)
	got := prepare(t, app, agent).Messages[0].Content[0].Text
	if len(got) > 1024 || !utf8.ValidString(got) || strings.Count(got, "</workspace-instructions>") != 1 || !strings.Contains(got, "truncated") {
		t.Fatalf("invalid bounded frame: %s", got)
	}
	persist(t, agent, prepare(t, app, agent))
	if len(prepare(t, app, agent).Messages) != 0 {
		t.Fatal("truncation digest instability")
	}
}
func TestRejectsSymlinksBinaryOversizeWrongCWDAndReplacedRoot(t *testing.T) {
	for _, kind := range []string{"symlink", "binary", "oversize", "wrong-cwd", "replaced-root"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			os.Mkdir(root, 0700)
			app, agent := fixture(t, root, 0)
			path := filepath.Join(root, "AGENTS.md")
			switch kind {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "secret")
				write(t, outside, "no")
				os.Symlink(outside, path)
			case "binary":
				write(t, path, "bad\x00data")
			case "oversize":
				write(t, path, strings.Repeat("x", (1<<20)+1))
			case "wrong-cwd":
				if app.CheckCWD(t.TempDir()) == nil {
					t.Fatal("wrong cwd accepted")
				}
				return
			case "replaced-root":
				os.Rename(root, root+".old")
				os.Mkdir(root, 0700)
				write(t, path, "new owner")
			}
			if _, err := app.prepare(context.Background(), harness.StepInput{Agent: agent}); err == nil {
				t.Fatal("unsafe file accepted")
			}
		})
	}
}

func TestMountedPromptAndJSONLResume(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), "repository guidance")
	requests := []harness.GenerateRequest{}
	h, err := harness.Boot(ctx, harness.Options{Adapter: harness.AdapterFunc(func(_ context.Context, req harness.GenerateRequest, _ func(harness.StreamChunk) error) (harness.Completion, error) {
		requests = append(requests, req)
		return harness.Completion{Message: harness.Message{Role: harness.RoleAssistant, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: "ok"}}}, Finish: harness.FinishStop}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	app, err := Mount(ctx, h, Options{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { h.Dispose(ctx); h.Drain(ctx); app.Close() }()
	p, err := harness.NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(ctx, harness.AgentOptions{ID: "resume-instructions", CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	detach, err := p.Attach(ctx, agent.Session())
	if err != nil {
		t.Fatal(err)
	}
	prompt := harness.Message{Role: harness.RoleUser, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: "continue"}}}
	if _, err := agent.Prompt(ctx, prompt); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatal("provider not called")
	}
	found := false
	for _, m := range requests[0].Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "repository guidance") {
				if m.Role != harness.RoleUser {
					t.Fatal("elevated role")
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing instruction hook")
	}
	if err := h.Agents().RemoveExact(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	restored, err := p.Load(ctx, "resume-instructions")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := h.Agents().Resume(ctx, restored, harness.AgentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepare(t, app, resumed).Messages) != 0 {
		t.Fatal("resume duplicated instructions")
	}
	write(t, filepath.Join(root, "AGENTS.md"), "changed while offline")
	if len(prepare(t, app, resumed).Messages) != 1 {
		t.Fatal("resume did not reconcile")
	}
}
