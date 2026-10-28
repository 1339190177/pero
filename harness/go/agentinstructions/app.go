// Package agentinstructions adapts Point's bounded instruction rendering to one
// explicitly selected workspace and the existing durable session surface.
package agentinstructions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/phodal/pero/harness/go/fstools"
	"github.com/phodal/pero/harness/go/harness"
	point "github.com/phodal/pero/harness/go/runtime"
)

const sourceKind = "pero.workspace.instructions.v1"

type source struct {
	Kind             string   `json:"kind"`
	Form             string   `json:"form,omitempty"`
	Baseline         bool     `json:"baseline,omitempty"`
	BaselineIdentity string   `json:"baselineIdentity,omitempty"`
	Changes          []change `json:"changes,omitempty"`
}

// Mount is opt-in. Files remain untrusted user-role context; this contribution
// never changes permissions, registers executable tools, or performs writes.
func Mount(ctx context.Context, h *harness.Harness, options Options) (*App, error) {
	if h == nil {
		return nil, errors.New("agent-instructions: harness required")
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if options.MaxBytes < 1024 || options.MaxBytes > DefaultMaxBytes {
		return nil, errors.New("agent-instructions: byte budget must be between 1024 and 65536")
	}
	files, err := fstools.OpenWorkspace(options.Workspace)
	if err != nil {
		return nil, err
	}
	app := &App{files: files, maxBytes: options.MaxBytes}
	definition := point.Define(point.Descriptor{ID: "harness/agent-instructions", Requires: []point.Requirement{point.Require(harness.AgentHooksKey)}}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		hooks, err := point.Resolve(run, harness.AgentHooksKey)
		if err != nil {
			return nil, err
		}
		_, err = hooks.PreStep.Use(run, func(ctx context.Context, input harness.StepInput, next point.Next[harness.StepInput]) (harness.StepInput, error) {
			prepared, err := app.prepare(ctx, input)
			if err != nil {
				return input, err
			}
			return next(prepared)
		})
		return nil, err
	})
	instance, err := h.Runtime().Mount(nil, "agent-instructions", definition, nil)
	if err == nil {
		err = instance.WaitStable(ctx)
	}
	if err != nil {
		if instance != nil {
			_ = instance.Dispose(context.Background())
		}
		_ = files.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) prepare(ctx context.Context, input harness.StepInput) (harness.StepInput, error) {
	if err := ctx.Err(); err != nil {
		return input, err
	}
	if input.Agent == nil {
		return input, errors.New("agent-instructions: agent required")
	}
	if err := a.CheckCWD(input.Agent.Session().WorkingDirectory()); err != nil {
		return input, err
	}
	files, err := a.read(ctx)
	if err != nil {
		return input, fmt.Errorf("agent-instructions: %w", err)
	}
	identity := fmt.Sprintf("%s:%d", a.files.Path(), a.maxBytes)
	currentDigest := ""
	if len(files) > 0 {
		currentDigest = digest(files[0].content)
	}
	// Derive exclusively from durable events. An aborted preparation must not
	// poison an in-memory cache and suppress the next successful admission.
	messages, err := input.Agent.Session().DeriveMessages()
	if err != nil {
		return input, err
	}
	previousIdentity, previousDigest, baseline := "", "", false
	for _, message := range messages {
		var previous source
		if json.Unmarshal(message.Source, &previous) != nil || previous.Kind != sourceKind || !previous.Baseline {
			continue
		}
		previousIdentity, previousDigest, baseline = previous.BaselineIdentity, "", true
		for _, item := range previous.Changes {
			if item.Action == "set" && item.Path == "AGENTS.md" {
				previousDigest = item.Digest
			}
		}
	}
	if baseline && previousIdentity == identity && previousDigest == currentDigest {
		return input, nil
	}
	if !baseline && len(files) == 0 {
		return input, nil
	}
	text, included := renderBaseline(files, a.maxBytes, baseline)
	if text == "" {
		return input, errors.New("agent-instructions: context budget too small")
	}
	changes := make([]change, 0, len(included))
	for _, file := range included {
		changes = append(changes, change{Action: "set", Scope: file.scope, Path: file.display, Digest: digest(file.content)})
	}
	raw, err := json.Marshal(source{Kind: sourceKind, Form: "instructions", Baseline: true, BaselineIdentity: identity, Changes: changes})
	if err != nil {
		return input, err
	}
	input.Messages = append(input.Messages, harness.Message{Role: harness.RoleUser, Source: raw, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: text}}})
	return input, nil
}
