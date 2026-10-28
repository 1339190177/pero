package main

import (
	"flag"
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
