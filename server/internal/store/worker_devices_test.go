package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func enrollmentRequest() WorkerEnrollmentRequest {
	return WorkerEnrollmentRequest{DeviceID: "device", RequestID: "enroll-1", Name: "Local laptop", Projects: []string{"team"}, Capabilities: []WorkerCapability{{ProjectID: "team", WorkspaceKey: "repo", AgentID: "fixture"}}, EnrollmentSecret: strings.Repeat("ab", 32)}
}

func TestWorkerDeviceEnrollment(t *testing.T) {
	client := &Client{ID: "client", Role: "client", Projects: []string{"team"}}
	for _, test := range []struct {
		name string
		edit func(*WorkerEnrollmentRequest)
		want error
	}{
		{"granted execution tuple", func(r *WorkerEnrollmentRequest) {}, nil},
		{"ungranted project", func(r *WorkerEnrollmentRequest) {
			r.Projects = []string{"private"}
			r.Capabilities[0].ProjectID = "private"
		}, ErrForbidden},
		{"capability outside requested projects", func(r *WorkerEnrollmentRequest) { r.Capabilities[0].ProjectID = "private" }, ErrEnrollmentInput},
		{"project without capabilities", func(r *WorkerEnrollmentRequest) { r.Capabilities = nil }, ErrEnrollmentInput},
		{"missing agent", func(r *WorkerEnrollmentRequest) { r.Capabilities[0].AgentID = "" }, ErrEnrollmentInput},
		{"secret too short", func(r *WorkerEnrollmentRequest) { r.EnrollmentSecret = "ab" }, ErrEnrollmentInput},
		{"invalid secret", func(r *WorkerEnrollmentRequest) { r.EnrollmentSecret = strings.Repeat("zz", 32) }, ErrEnrollmentInput},
		{"control in identity", func(r *WorkerEnrollmentRequest) { r.DeviceID = "device\t" }, ErrEnrollmentInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := enrollmentRequest()
			test.edit(&request)
			_, err := request.Authorize(client)
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	request := enrollmentRequest()
	request.Projects = append(request.Projects, "team")
	request.Capabilities = append(request.Capabilities, request.Capabilities[0])
	canonical, err := request.Authorize(client)
	if err != nil || len(canonical.request.Projects) != 1 || len(canonical.request.Capabilities) != 1 {
		t.Fatal("duplicate capabilities did not canonicalize", err)
	}
	if _, err = request.Authorize(&Client{ID: "new-user", Role: "client"}); !errors.Is(err, ErrForbidden) {
		t.Fatal("new login gained project rights", err)
	}
}

func TestWorkerEnrollmentReceiptsAndCapabilityClaims(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	client, _, err := s.CreateClient(ctx, "Owner", []string{"team", "other"})
	if err != nil {
		t.Fatal(err)
	}
	request := enrollmentRequest()
	enroll := func(request WorkerEnrollmentRequest) (Worker, string, error) {
		authorized, err := request.Authorize(&client)
		if err != nil {
			return Worker{}, "", err
		}
		return s.EnrollWorker(ctx, authorized)
	}
	worker, token, err := enroll(request)
	if err != nil {
		t.Fatal(err)
	}
	again, recovered, err := enroll(request)
	if err != nil || again.ID != worker.ID || recovered != token {
		t.Fatal("lost enrollment response not recovered", err)
	}
	if id, err := s.AuthenticateWorker(ctx, token); err != nil || id != worker.ID {
		t.Fatal("worker credential invalid", err)
	}
	if _, err := s.AuthenticateClient(ctx, token); !errors.Is(err, ErrForbidden) {
		t.Fatal("worker credential authenticated as user", err)
	}
	changed := request
	changed.Name = "Changed under same receipt"
	if _, _, err := enroll(changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed enrollment replay accepted", err)
	}
	rotated := request
	rotated.RequestID = "enroll-2"
	rotated.EnrollmentSecret = strings.Repeat("cd", 32)
	current, currentToken, err := enroll(rotated)
	if err != nil || current.ID != worker.ID {
		t.Fatal("stable device duplicated", err)
	}
	if _, err := s.AuthenticateWorker(ctx, token); !errors.Is(err, ErrForbidden) {
		t.Fatal("rotated token still valid", err)
	}
	if _, _, err := enroll(request); !errors.Is(err, ErrConflict) {
		t.Fatal("superseded receipt restored old config", err)
	}
	specs := []Spec{testSpec("team"), testSpec("team"), testSpec("team"), testSpec("other")}
	specs[0].AgentID = "missing-agent"
	specs[1].WorkspaceKey = "missing-workspace"
	var supported string
	for i, spec := range specs {
		task, err := s.CreateTask(ctx, spec, "")
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.Dispatch(ctx, task.ID, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			supported = run.ID
		}
	}
	run, err := s.Claim(ctx, worker.ID, "process", "claim-1")
	if err != nil || run == nil || run.ID != supported {
		t.Fatalf("unsupported run consumed: %+v %v", run, err)
	}
	if _, _, err := enroll(WorkerEnrollmentRequest{DeviceID: request.DeviceID, RequestID: "busy-change", Name: request.Name, Projects: request.Projects, Capabilities: request.Capabilities, EnrollmentSecret: request.EnrollmentSecret}); !errors.Is(err, ErrConflict) {
		t.Fatal("active device reconfigured", err)
	}
	if err := s.DisableOwnedWorker(ctx, client.ID, worker.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("active device disabled", err)
	}
	if _, err := s.Complete(ctx, run.ID, worker.ID, "process", run.Generation, Completion{Outcome: "succeeded", Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableOwnedWorker(ctx, "another-owner", worker.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign device disabled", err)
	}
	if err := s.DisableOwnedWorker(ctx, client.ID, worker.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateWorker(ctx, currentToken); !errors.Is(err, ErrForbidden) {
		t.Fatal("disabled worker credential valid", err)
	}
	if _, err := s.Claim(ctx, worker.ID, "process", "disabled-claim"); !errors.Is(err, ErrForbidden) {
		t.Fatal("disabled worker claimed", err)
	}

	reenabled := rotated
	reenabled.RequestID = "enroll-3"
	if _, _, err := enroll(reenabled); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, testSpec("team"), "")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.Dispatch(ctx, task.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE clients SET projects=ARRAY['other'] WHERE id=$1`, client.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.Claim(ctx, worker.ID, "process-2", "revoked-grant"); err != nil || claimed != nil {
		t.Fatal("revoked owner project still claimed", err)
	}
	var queuedStatus string
	if err = s.pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, queued.ID).Scan(&queuedStatus); err != nil || queuedStatus != "queued" {
		t.Fatal("ungranted Run was leased", err)
	}
	legacy, _ := mustWorker(t, s, "team")
	legacyRun, err := s.Claim(ctx, legacy.ID, "legacy", "legacy-claim")
	if err != nil || legacyRun == nil || legacyRun.Spec.AgentID != "missing-agent" {
		t.Fatal("legacy worker capability behavior changed", err)
	}
}
