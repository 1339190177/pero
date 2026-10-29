package main

import (
	"flag"
	"github.com/phodal/pero/harness/go/sandbox"
	"testing"
)

func TestSandboxFlags(t *testing.T) {
	for _, tc := range []struct {
		args                []string
		workspace, approval string
		shell, fail         bool
	}{
		{nil, "", "ask", false, false},
		{[]string{"--sandbox-runtime=/usr/bin"}, "/work", "ask", true, true},
		{[]string{"--sandbox=danger-full-access", "--sandbox-runtime=/usr/bin"}, "/work", "ask", true, true},
		{[]string{"--sandbox=strict-read-only", "--sandbox-runtime=/usr/bin"}, "/work", "never", true, true},
		{[]string{"--sandbox=strict-read-only", "--sandbox-runtime=/usr/bin"}, "/work", "ask", false, true},
		{[]string{"--sandbox=strict-read-only"}, "/work", "ask", true, true},
		{[]string{"--sandbox=strict-read-only", "--sandbox-runtime=/usr/bin"}, "/work", "ask", true, false},
		{[]string{"--sandbox=windows-acl-workspace-write"}, "C:\\work", "ask", true, false},
		{[]string{"--sandbox=windows-acl-read-only"}, "C:\\work", "ask", true, false},
		{[]string{"--sandbox=windows-acl-read-only", "--sandbox-runtime=/usr/bin"}, "C:\\work", "ask", true, true},
		{[]string{"--sandbox=windows-acl-read-only"}, "C:\\work", "never", true, true},
		{[]string{"--sandbox=windows-acl-read-only"}, "C:\\work", "ask", false, true},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		o := bindSandbox(fs)
		if e := fs.Parse(tc.args); e != nil {
			t.Fatal(e)
		}
		_, e := o.config(tc.workspace, tc.shell, tc.approval)
		if (e != nil) != tc.fail {
			t.Fatalf("%v: %v", tc.args, e)
		}
	}
}

func TestWindowsSandboxPresetPreservesPartialChoice(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o := bindSandbox(fs)
	if err := fs.Parse([]string{"--sandbox=windows-acl-workspace-write"}); err != nil {
		t.Fatal(err)
	}
	c, err := o.config(`C:\work`, true, "ask")
	if err != nil || c.Backend != sandbox.BackendWindowsACL || c.Mode != sandbox.WorkspaceWrite || len(c.RuntimePaths) != 0 {
		t.Fatalf("config=%+v err=%v", c, err)
	}
}
