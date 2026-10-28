package agentinstructions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type instructionFile struct{ abs, display, scope, content string }

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func splitScope(scope string) (string, string) {
	index := strings.IndexByte(scope, 0)
	if index < 0 {
		return scope, ""
	}
	return scope[:index], scope[index+1:]
}

func (a *App) read(ctx context.Context) ([]instructionFile, error) {
	// The shared reader intentionally reports a generic error for missing
	// manifest entries. Probe only absence, never read bytes by pathname; both
	// admission and every actual read remain descriptor-rooted.
	if err := a.files.CheckCWD(a.files.Path()); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(filepath.Join(a.files.Path(), "AGENTS.md")); errors.Is(err, os.ErrNotExist) {
		if err := a.files.CheckCWD(a.files.Path()); err != nil {
			return nil, err
		}
		return nil, nil
	}
	value, err := a.files.Read(ctx, "AGENTS.md")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []instructionFile{{display: "AGENTS.md", scope: ".\x00AGENTS.md", content: value.Text}}, nil
}
