package agentinstructions

import "github.com/phodal/pero/harness/go/fstools"

const DefaultMaxBytes = 65_536

// Options is trusted host composition, never model input. Discovery is exactly
// Workspace/AGENTS.md; no home, parent, alternate, or recursive paths are read.
type Options struct {
	Workspace string
	// MaxBytes is the rendered context budget; zero selects DefaultMaxBytes.
	MaxBytes int
}

type App struct {
	files    *fstools.Workspace
	maxBytes int
}

// Close follows Harness Dispose/Drain, so no instruction read is in flight.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	return a.files.Close()
}
func (a *App) CheckCWD(cwd string) error { return a.files.CheckCWD(cwd) }
