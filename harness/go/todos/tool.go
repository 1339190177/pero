// Package todos provides the DSH-compatible whole-list todo tool and durable
// Session projection. Every write is a complete replacement; the next turn
// clears the previous standing plan until the model writes a new list.
package todos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/phodal/pero/harness/go/harness"
	harnesstool "github.com/phodal/pero/harness/go/tool"
	point "github.com/phodal/pero/harness/go/runtime"
)

const ToolName = "todo_write"

// Canonical output is independent of the model-facing presentation.
const todoOutputSchema = `{"type":"object","additionalProperties":false,"properties":{"todos":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["content","status"]}},"counts":{"type":"object","additionalProperties":false,"properties":{"pending":{"type":"integer"},"inProgress":{"type":"integer"},"completed":{"type":"integer"}},"required":["pending","inProgress","completed"]}},"required":["todos","counts"]}`

type todoToolValue struct {
	Todos  []Item         `json:"todos"`
	Counts todoToolCounts `json:"counts"`
}

type todoToolCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Completed  int `json:"completed"`
}

func renderTodoOutput(_ context.Context, _ harness.ToolRequest, raw json.RawMessage) (harness.ToolOutputProjection, error) {
	// Canonical rewrites follow JSON's numeric domain, not native count storage.
	// In particular, integer-valued 1.0, 0e0 and values beyond Go int are valid.
	var value struct {
		Counts struct {
			Pending    float64 `json:"pending"`
			InProgress float64 `json:"inProgress"`
			Completed  float64 `json:"completed"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return harness.ToolOutputProjection{}, err
	}
	counts := make([]string, 3)
	for index, count := range []float64{value.Counts.Pending, value.Counts.InProgress, value.Counts.Completed} {
		// encoding/json uses ECMAScript-compatible finite-number formatting.
		rendered, err := json.Marshal(count)
		if err != nil {
			return harness.ToolOutputProjection{}, err
		}
		counts[index] = string(rendered)
	}
	text := fmt.Sprintf("Updated todo list: %s pending, %s in progress, %s completed.", counts[0], counts[1], counts[2])
	return harness.ToolOutputProjection{Content: []harness.ContentBlock{{Type: harness.BlockText, Text: text}}}, nil
}

type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
)

type Item struct {
	Content string `json:"content"`
	Status  Status `json:"status"`
}

type Options struct {
	// ArgumentErrorMode is trusted diagnostic wiring, never model/profile JSON.
	// It is independent of content normalization and does not alter admission.
	ArgumentErrorMode harness.ToolArgumentErrorMode `json:"-"`
	// ContentNormalization is trusted composition wiring, never model/profile JSON.
	// It also selects DSH last-member decoding after admission and the durable
	// todo/write interpretation for new snapshots.
	ContentNormalization    ContentNormalizationMode `json:"-"`
	AllowParallelInProgress bool
	// AllowAdditionalInputProperties preserves alpha.3 root input openness.
	// Trusted DSH composition only; nested todo items remain closed.
	AllowAdditionalInputProperties bool `json:"-"`
	// ToolWrapper is trusted composition wiring, never profile configuration.
	ToolWrapper func(harness.Tool) harness.Tool
}

type writeArguments struct {
	Todos []Item `json:"todos"`
}

type writeEvent struct {
	Todos []Item `json:"todos"`
}

func Mount(ctx context.Context, h *harness.Harness, options Options) (*point.Instance, error) {
	if h == nil {
		return nil, errors.New("todos: harness is required")
	}
	if options.ArgumentErrorMode != harness.ArgumentErrorsLegacy && options.ArgumentErrorMode != harness.ArgumentErrorsDSHAlpha3Todo {
		return nil, errors.New("todos: unsupported argument error mode")
	}
	if options.ContentNormalization != ContentNormalizationLegacy && options.ContentNormalization != ContentNormalizationDSHAlpha3 {
		return nil, errors.New("todos: unsupported content normalization mode")
	}
	description := "Record and update a structured task list for the current work. Send the ENTIRE list every call; it replaces the previous list. Add one item per concrete step for multi-step work, update completion promptly, and skip this tool for trivial single-step tasks."
	if options.AllowParallelInProgress {
		description += " Mark every task currently being worked on in_progress; parallel work may have several active items."
	} else {
		description += " Keep at most one item in_progress at a time."
	}
	tool := harness.Tool{
		Schema: harness.ToolSchema{
			Name: ToolName, Description: description,
			OutputSchema:     json.RawMessage(todoOutputSchema),
			OutputProjection: "dsh.todo.v1",
			InputSchema:      json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["content","status"],"additionalProperties":false}}},"required":["todos"],"additionalProperties":%t}`, options.AllowAdditionalInputProperties)),
		},
		ArgumentErrorMode: options.ArgumentErrorMode,
		ExecutionMode:     harness.ExecutionSerial,
		RenderOutput:      renderTodoOutput,
		ExecuteRequest: func(_ context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
			if request.Agent == nil {
				return harness.ToolResult{}, errors.New("todo_write requires an owning agent session")
			}
			raw := request.Arguments
			if options.AllowAdditionalInputProperties {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					return harness.ToolResult{}, err
				}
				// JSON field names are case-sensitive; Go struct aliases must not
				// let an unrelated TODOS property replace the declared list.
				raw, _ = json.Marshal(map[string]json.RawMessage{"todos": fields["todos"]})
			}
			if err := harnesstool.ValidateNativeJSONDecoding(raw); err != nil {
				return harness.ToolResult{}, err
			}
			var arguments writeArguments
			if options.ContentNormalization == ContentNormalizationDSHAlpha3 {
				var err error
				arguments, err = decodeDSHWriteArguments(raw)
				if err != nil {
					return harness.ToolResult{}, err
				}
			} else if err := json.Unmarshal(raw, &arguments); err != nil {
				return harness.ToolResult{}, err
			}
			todos, counts, err := normalizeWithMode(arguments.Todos, options.AllowParallelInProgress, options.ContentNormalization)
			if err != nil {
				if domain, ok := err.(*todoDomainError); ok && options.ContentNormalization == ContentNormalizationDSHAlpha3 {
					return dshTodoFailure(domain), nil
				}
				return harness.ToolResult{}, err
			}
			var event any = writeEvent{Todos: todos}
			if options.ContentNormalization == ContentNormalizationDSHAlpha3 {
				event = normalizedWriteEvent{Todos: todos, Normalization: ecmascriptTrimV1}
			}
			if _, err := request.Agent.Session().Append("todo/write", event); err != nil {
				return harness.ToolResult{}, err
			}
			value, err := json.Marshal(todoToolValue{Todos: todos, Counts: todoToolCounts{Pending: counts[Pending], InProgress: counts[InProgress], Completed: counts[Completed]}})
			if err != nil {
				return harness.ToolResult{}, err
			}
			projection, err := renderTodoOutput(context.Background(), request, value)
			return harness.ToolResult{Value: value, Content: projection.Content}, err
		},
	}
	if options.ContentNormalization == ContentNormalizationDSHAlpha3 {
		tool.ErrorOutputMode = harness.ErrorOutputDSHAlpha3
		tool = harness.WithDSHTodoNumberCompatibility(tool)
	}
	if options.ToolWrapper != nil {
		tool = options.ToolWrapper(tool)
	}
	return h.RegisterTool(ctx, "todos/tool", tool)
}

// decodeDSHWriteArguments runs only after schema admission and the existing
// lossless-JSON guard. Select final exact members before typed decoding, matching
// JSON.parse without rewriting the original request or accepting Go aliases.
func decodeDSHWriteArguments(raw json.RawMessage) (writeArguments, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return writeArguments{}, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(root["todos"], &items); err != nil {
		return writeArguments{}, err
	}
	arguments := writeArguments{Todos: make([]Item, len(items))}
	for index, rawItem := range items {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &fields); err != nil {
			return writeArguments{}, err
		}
		if err := json.Unmarshal(fields["content"], &arguments.Todos[index].Content); err != nil {
			return writeArguments{}, err
		}
		if err := json.Unmarshal(fields["status"], &arguments.Todos[index].Status); err != nil {
			return writeArguments{}, err
		}
	}
	return arguments, nil
}

// Current folds the latest valid whole-list snapshot after the most recent
// turn/start. The returned slice is detached from the durable event bytes.
func Current(session *harness.Session) ([]Item, bool, error) {
	if session == nil {
		return nil, false, errors.New("todos: session is required")
	}
	var current []Item
	found := false
	mode := ContentNormalizationLegacy
	for _, event := range session.Events() {
		switch event.Type {
		case "turn/start":
			current = nil
			found = false
		case "todo/write":
			payload, format, err := decodeWriteEvent(event.Data)
			if err != nil {
				return nil, false, fmt.Errorf("todos: decode todo/write at seq %d: %w", event.Seq, err)
			}
			normalized, _, err := normalizeWithMode(payload.Todos, true, format)
			if err != nil {
				return nil, false, fmt.Errorf("todos: invalid todo/write at seq %d: %w", event.Seq, err)
			}
			current = normalized
			mode = format
			found = true
		}
	}
	if found && mode == ContentNormalizationDSHAlpha3 {
		return append(make([]Item, 0, len(current)), current...), true, nil
	}
	return append([]Item(nil), current...), found, nil
}

