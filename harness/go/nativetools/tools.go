// Package nativetools is explicit headless composition, not a sandbox or a
// product profile. Read authority is granted by the configured workspace;
// mutations and every shell invocation additionally require ACP approval.
package nativetools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/phodal/pero/harness/go/fstools"
	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/shell"
)

type Options struct {
	Workspace   string
	EnableShell bool
	Shell       string
}
type App struct{ files *fstools.Workspace }

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	return a.files.Close()
}

// CheckCWD reuses the launch-time workspace authority for early ACP admission.
// Tool bodies still recheck it before execution to catch later changes.
func (a *App) CheckCWD(cwd string) error {
	if a == nil || a.files == nil {
		return errors.New("native workspace is unavailable")
	}
	return a.files.CheckCWD(cwd)
}

type arguments struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all"`
	Query      string `json:"query"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	Command    string `json:"command"`
	TimeoutMS  int    `json:"timeout_ms"`
}

func Mount(ctx context.Context, h *harness.Harness, opts Options) (app *App, resultErr error) {
	if h == nil {
		return nil, errors.New("native tools require harness")
	}
	files, err := fstools.OpenWorkspace(opts.Workspace)
	if err != nil {
		return nil, err
	}
	app = &App{files: files}
	defer func() {
		if resultErr != nil {
			_ = files.Close()
		}
	}()
	definitions := []struct {
		name, description, properties, required string
		approval                                bool
		run                                     func(context.Context, arguments) (any, error)
	}{
		{"read_file", "Read bounded UTF-8 text from an existing workspace-relative file. offset is a 1-based line number.", `"path":{"type":"string"},"offset":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":2000}`, `"path"`, false, func(c context.Context, a arguments) (any, error) {
			v, e := files.Read(c, a.Path)
			if e != nil {
				return nil, e
			}
			return fstools.FormatRead(v, a.Offset, a.Limit)
		}},
		{"write_file", "Create or replace a bounded UTF-8 file under the configured workspace. Parent directory must already exist. Requires approval.", `"path":{"type":"string"},"content":{"type":"string","maxLength":1048576}`, `"path","content"`, true, func(c context.Context, a arguments) (any, error) { return files.Write(c, a.Path, a.Content) }},
		{"edit_file", "Replace exact text in an existing workspace file; old_text must match once unless replace_all. Requires approval.", `"path":{"type":"string"},"old_text":{"type":"string","minLength":1,"maxLength":1048576},"new_text":{"type":"string","maxLength":1048576},"replace_all":{"type":"boolean"}`, `"path","old_text","new_text"`, true, func(c context.Context, a arguments) (any, error) {
			return files.Edit(c, a.Path, a.OldText, a.NewText, a.ReplaceAll)
		}},
		{"list_directory", "List up to 256 direct workspace directory entries; does not follow symlinks.", `"path":{"type":"string"}`, ``, false, func(c context.Context, a arguments) (any, error) { return files.List(c, a.Path) }},
		{"search_files", "Find literal query text in bounded workspace files. At most 512 entries, 8 MiB text and 64 matches; symlinks skipped.", `"path":{"type":"string"},"query":{"type":"string","minLength":1,"maxLength":4096}`, `"query"`, false, func(c context.Context, a arguments) (any, error) { return files.Search(c, a.Path, a.Query) }},
	}
	if opts.EnableShell {
		if opts.Shell == "" {
			opts.Shell = "/bin/bash"
		}
		executor, e := shell.NewLocalExecutor(shell.LocalOptions{Shell: opts.Shell, CWD: files.Path(), Timeout: 30 * time.Second, MaxTimeout: 120 * time.Second, MaxOutputBytes: 16 << 10, Grace: 200 * time.Millisecond, BaseEnv: []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}})
		if e != nil {
			return nil, e
		}
		definitions = append(definitions, struct {
			name, description, properties, required string
			approval                                bool
			run                                     func(context.Context, arguments) (any, error)
		}{"shell", "Run an approved foreground Bash command with a minimal environment, bounded output and timeout. NOT an OS sandbox: may access outside workspace or network. No background jobs.", `"command":{"type":"string","minLength":1,"maxLength":16384},"timeout_ms":{"type":"integer","minimum":1,"maximum":120000}`, `"command"`, true, func(c context.Context, a arguments) (any, error) {
			if e := files.CheckCWD(files.Path()); e != nil {
				return nil, e
			}
			spec, e := executor.Resolve(shell.ExecRequest{Command: a.Command, Workdir: files.Path(), Timeout: time.Duration(a.TimeoutMS) * time.Millisecond})
			if e != nil {
				return nil, e
			}
			result, runErr := executor.Run(c, spec)
			if runErr == nil && result.ExitCode != nil && *result.ExitCode != 0 {
				runErr = fmt.Errorf("shell exited with code %d", *result.ExitCode)
			}
			return result, runErr
		}})
	}
	// Registered contributions are owned by the Harness and drain before App.Close.
	for _, d := range definitions {
		schema := json.RawMessage(fmt.Sprintf(`{"type":"object","additionalProperties":false,"properties":{%s},"required":[%s]}`, d.properties, d.required))
		_, err = h.RegisterTool(ctx, "native/"+d.name, harness.Tool{Schema: harness.ToolSchema{Name: d.name, Description: d.description, InputSchema: schema}, RequiresApproval: d.approval, Timeout: 125 * time.Second, ExecutionMode: harness.ExecutionSerial, ExecuteRequest: func(c context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
			if request.Agent == nil {
				return harness.ToolResult{}, errors.New("native tools require an agent")
			}
			if e := files.CheckCWD(request.Agent.Session().WorkingDirectory()); e != nil {
				return harness.ToolResult{}, e
			}
			var a arguments
			if e := json.Unmarshal(request.Arguments, &a); e != nil {
				return harness.ToolResult{}, e
			}
			value, operationErr := d.run(c, a)
			return renderResult(value, operationErr)

		}})
		if err != nil {
			return nil, err
		}
	}
	return app, nil
}

func boundedText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
func renderResult(value any, operationErr error) (harness.ToolResult, error) {
	// JSON escaping may multiply captured shell bytes by six. Keep status and
	// receipts even when textual output must be reduced further.
	switch v := value.(type) {
	case shell.RunResult:
		if len(v.Stdout.Text) > 4096 {
			v.Stdout.Text = boundedText(v.Stdout.Text, 4096)
			v.Stdout.Truncated = true
		}
		if len(v.Stderr.Text) > 4096 {
			v.Stderr.Text = boundedText(v.Stderr.Text, 4096)
			v.Stderr.Truncated = true
		}
		value = v
	case fstools.ScopedTextReceipt:
		v.Path = boundedText(v.Path, 1024)
		value = v
	}
	output := value
	if operationErr != nil {
		output = map[string]any{"result": value, "error": boundedText(operationErr.Error(), 1024), "warning": "An operation can have partial effects; inspect reported state before retrying. Unknown effects must be reconciled, not blindly retried."}
	}
	raw, e := json.Marshal(output)
	if e != nil {
		return harness.ToolResult{}, e
	}
	if len(raw) > fstools.MaxOutputBytes {
		// Read/list/search can have huge escaped names. This fallback never asserts
		// completion or discards a mutation receipt/shell status (bounded above).
		fallback := map[string]any{"truncated": true, "message": "Result exceeds encoded output budget; narrow the request. Do not infer mutation completion from this response."}
		if operationErr != nil {
			fallback["error"] = boundedText(operationErr.Error(), 1024)
		}
		raw, e = json.Marshal(fallback)
		if e != nil {
			return harness.ToolResult{}, e
		}
	}
	return harness.ToolResult{IsError: operationErr != nil, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: string(raw)}}}, nil
}
