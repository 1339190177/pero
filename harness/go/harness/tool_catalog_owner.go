package harness

import (
	"errors"

	point "github.com/phodal/pero/harness/go/runtime"
)

// ToolCatalogOwner binds definitions produced by one trusted dynamic catalog to
// its exact runtime activation. It does not own the catalog's provider, Agent,
// or accepted background work. Ordinary ReplaceGeneration snapshots retain
// their existing lifetime unless their producer explicitly uses this receipt.
type ToolCatalogOwner struct{ owner *toolOwner }

// NewCatalogOwner captures this exact registry as resolved from run's runtime.
// Activation cleanup cancels and joins wrapped callbacks before resources are
// released. Callers must not synchronously join their own activation cleanup.
func (r *ToolRegistry) NewCatalogOwner(run *point.Run) (*ToolCatalogOwner, error) {
	if r == nil || run == nil || !run.Active() || run.Scope == nil {
		return nil, point.ErrInactiveRun
	}
	if r.runtime == nil || run.Scope.Runtime() != r.runtime {
		return nil, errors.New("harness: catalog owner requires the registry's constructor runtime")
	}
	actual, err := point.Resolve(run, ToolRegistryKey)
	if err != nil {
		return nil, err
	}
	if actual != r {
		return nil, errors.New("harness: catalog owner requires this Run's exact tool registry")
	}
	owner := newToolOwner(r, run)
	if err := owner.admissionError(); err != nil {
		return nil, err
	}
	if err := run.BeforeCleanup("tool-catalog-owner", owner.drain); err != nil {
		return nil, err
	}
	return &ToolCatalogOwner{owner: owner}, nil
}

// Active is observational. Catalog producers recheck it after any synchronous
// callback, before returning schemas or definitions. It grants no body admission.
func (o *ToolCatalogOwner) Active() bool {
	return o != nil && o.owner != nil && o.owner.admissionError() == nil
}

// WrapTool gives a newly constructed leaf definition the captured activation's
// cancellation, callback drain and final-result fence. Another owner's captured
// definition cannot be rebound through this receipt.
func (o *ToolCatalogOwner) WrapTool(tool Tool) (Tool, error) {
	if !o.Active() {
		return Tool{}, ErrToolOwnerRetired
	}
	if tool.owner != nil {
		return Tool{}, errors.New("harness: catalog definition already belongs to an exact owner")
	}
	if tool.dispatchCapture != nil || tool.capturedAgent != nil {
		return Tool{}, errors.New("harness: catalog owner requires a fresh leaf definition")
	}
	return o.owner.wrap(tool), nil
}
