package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestCredentialCannotBeInheritedUnderCustomName(t *testing.T) {
	const name = "MY_CREDENTIAL"
	const dummy = "local-fixture-not-a-secret"
	t.Setenv(name, dummy)
	got, err := takeProviderCredential(name)
	if err != nil || got != dummy {
		t.Fatalf("credential read failed: %v", err)
	}
	if _, exists := os.LookupEnv(name); exists {
		t.Fatal("credential retained in process environment")
	}
	// This is the environment inherited by a default stdio MCP child.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestCredentialChildProcess$")
	child.Env = append(os.Environ(), "AGENT_CREDENTIAL_CHILD_TEST=1")
	_, err = child.Output()
	if err != nil {
		t.Fatal(err)
	}

}

func TestCredentialChildProcess(t *testing.T) {
	if os.Getenv("AGENT_CREDENTIAL_CHILD_TEST") != "1" {
		return
	}
	if _, exists := os.LookupEnv("MY_CREDENTIAL"); exists {
		os.Exit(19)
	}
}
