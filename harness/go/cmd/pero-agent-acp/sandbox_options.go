package main

import (
	"errors"
	"flag"
	"github.com/phodal/pero/harness/go/permissionpresets"
	"github.com/phodal/pero/harness/go/sandbox"
)

type sandboxFlags struct {
	preset string
	paths  runtimePaths
}
type runtimePaths []string

func (p *runtimePaths) String() string { return "explicit trusted runtime mounts" }
func (p *runtimePaths) Set(s string) error {
	if s == "" {
		return errors.New("empty runtime mount")
	}
	*p = append(*p, s)
	return nil
}
func bindSandbox(fs *flag.FlagSet) *sandboxFlags {
	o := &sandboxFlags{}
	fs.StringVar(&o.preset, "sandbox", "", "opt-in strict-read-only/strict-workspace-write or Windows-only windows-acl-read-only/windows-acl-workspace-write (PARTIAL: reads/network unrestricted); requires shell and approval ask")
	fs.Var(&o.paths, "sandbox-runtime", "trusted absolute runtime file/tree (repeat; no implicit mounts)")
	return o
}
func (o *sandboxFlags) config(workspace string, shell bool, approval string) (*sandbox.Config, error) {
	if o.preset == "" {
		if len(o.paths) > 0 {
			return nil, errors.New("--sandbox-runtime requires --sandbox")
		}
		return nil, nil
	}
	if workspace == "" || !shell || approval != "ask" {
		return nil, errors.New("--sandbox requires --workspace, --enable-shell and --approval=ask")
	}
	p, e := permissionpresets.Resolve(o.preset)
	if e != nil {
		return nil, e
	}
	if p.Backend == sandbox.BackendWindowsACL {
		if len(o.paths) != 0 {
			return nil, errors.New("Windows ACL sandbox does not isolate reads and cannot accept --sandbox-runtime")
		}
	} else if len(o.paths) < 1 || len(o.paths) > 32 {
		return nil, errors.New("strict sandbox requires 1–32 runtime mounts")
	}
	return &sandbox.Config{Backend: p.Backend, Mode: p.Sandbox, RuntimePaths: append([]string(nil), o.paths...)}, nil
}
