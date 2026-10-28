package httpapi_test

import (
	"testing"

	"github.com/phodal/pero/server/internal/store"
)

func TestScopedClientIdentityHumanClaimAndWorkerLease(t *testing.T) {
	server := testAPI(t)
	type credentials struct {
		Client store.Client `json:"client"`
		Token  string       `json:"token"`
	}
	alice := parse[credentials](t, request(t, server, "POST", "/api/v1/clients", "test-operator", map[string]any{"name": "Alice", "projects": []string{"team"}}, "", 201))
	bob := parse[credentials](t, request(t, server, "POST", "/api/v1/clients", "test-operator", map[string]any{"name": "Bob", "projects": []string{"team"}}, "", 201))
	me := parse[store.Client](t, request(t, server, "GET", "/api/client/v1/me", alice.Token, nil, "", 200))
	if me.ID != alice.Client.ID || me.Name != "Alice" || me.Role != "client" || len(me.Projects) != 1 || me.Projects[0] != "team" {
		t.Fatalf("scoped identity: %+v", me)
	}
	operator := parse[map[string]any](t, request(t, server, "GET", "/api/v1/me", "test-operator", nil, "", 200))
	if operator["role"] != "operator" {
		t.Fatalf("operator identity: %+v", operator)
	}
	request(t, server, "GET", "/api/client/v1/me", "test-operator", nil, "", 401)
	request(t, server, "GET", "/api/v1/tasks", alice.Token, nil, "", 401)
	request(t, server, "POST", "/api/v1/clients", alice.Token, map[string]any{"name": "Eve", "projects": []string{"private"}}, "", 401)
	request(t, server, "POST", "/api/worker/v1/claims", alice.Token, map[string]any{"request_id": "x", "instance_id": "x"}, "", 401)
	createTask := func(project string) store.Task {
		return parse[store.Task](t, request(t, server, "POST", "/api/v1/tasks", "test-operator", store.Spec{Title: "Shared goal", Prompt: "Complete fixture", Acceptance: "Fixture completes", ProjectID: project, WorkspaceKey: "repo", AgentID: "fixture"}, "", 201))
	}
	task := createTask("team")
	private := createTask("private")
	page := parse[store.TaskPage](t, request(t, server, "GET", "/api/client/v1/tasks", alice.Token, nil, "", 200))
	if len(page.Tasks) != 1 || page.Tasks[0].ID != task.ID {
		t.Fatalf("private task in scoped list: %+v", page)
	}
	request(t, server, "GET", "/api/client/v1/tasks?cursor="+private.ID, alice.Token, nil, "", 404)
	for _, action := range []string{"claim", "release", "dispatch", "cancel"} {
		request(t, server, "POST", "/api/client/v1/tasks/"+private.ID+"/"+action, alice.Token, map[string]any{}, "shared-receipt", 404)
	}
	request(t, server, "GET", "/api/client/v1/tasks/"+private.ID, alice.Token, nil, "", 404)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/claim", alice.Token, map[string]any{"owner": bob.Client.ID}, "", 400)
	claimed := parse[store.Task](t, request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/claim", alice.Token, map[string]any{}, "", 200))
	if claimed.ClaimedBy == nil || claimed.ClaimedBy.ID != alice.Client.ID || claimed.ClaimedBy.Name != "Alice" {
		t.Fatalf("human claim: %+v", claimed)
	}
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/claim", alice.Token, map[string]any{}, "", 200)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/claim", bob.Token, map[string]any{}, "", 409)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/release", bob.Token, map[string]any{}, "", 403)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/dispatch", bob.Token, map[string]any{}, "", 403)
	visible := parse[struct {
		Task store.Task `json:"task"`
	}](t, request(t, server, "GET", "/api/v1/tasks/"+task.ID, "test-operator", nil, "", 200))
	if visible.Task.ClaimedBy == nil || visible.Task.ClaimedBy.ID != alice.Client.ID {
		t.Fatalf("operator claim hydration: %+v", visible)
	}
	type workerCredentials struct {
		Worker store.Worker `json:"worker"`
		Token  string       `json:"token"`
	}
	worker := parse[workerCredentials](t, request(t, server, "POST", "/api/v1/workers", "test-operator", map[string]any{"name": "Team Worker", "projects": []string{"team", "hidden"}}, "", 201))
	foreign := parse[workerCredentials](t, request(t, server, "POST", "/api/v1/workers", "test-operator", map[string]any{"name": "Private Worker", "projects": []string{"private"}}, "", 201))
	request(t, server, "GET", "/api/client/v1/tasks", worker.Token, nil, "", 401)
	workers := parse[struct {
		Workers []store.Worker `json:"workers"`
	}](t, request(t, server, "GET", "/api/client/v1/workers", alice.Token, nil, "", 200))
	if len(workers.Workers) != 1 || workers.Workers[0].ID != worker.Worker.ID || len(workers.Workers[0].Projects) != 1 {
		t.Fatalf("private worker metadata leaked: %+v", workers)
	}
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/dispatch", alice.Token, map[string]any{"worker_id": foreign.Worker.ID}, "", 403)
	dispatched := parse[store.Run](t, request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/dispatch", alice.Token, map[string]any{"worker_id": worker.Worker.ID}, "delivery", 200))
	replay := parse[store.Run](t, request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/dispatch", alice.Token, map[string]any{"worker_id": worker.Worker.ID}, "delivery", 200))
	if dispatched.ID != replay.ID {
		t.Fatal("client dispatch replay created another Run")
	}
	lease := parse[struct {
		Run *store.Run `json:"run"`
	}](t, request(t, server, "POST", "/api/worker/v1/claims", worker.Token, map[string]any{"request_id": "worker-delivery", "instance_id": "process"}, "", 200))
	if lease.Run == nil || lease.Run.ID != dispatched.ID {
		t.Fatalf("Worker did not acquire Run: %+v", lease)
	}
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/release", alice.Token, map[string]any{}, "", 409)
	// A human release conflict must not revoke or replace the independent Worker lease.
	request(t, server, "POST", "/api/worker/v1/runs/"+dispatched.ID+"/renew", worker.Token, map[string]any{"generation": lease.Run.Generation, "instance_id": "process"}, "", 200)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/cancel", bob.Token, map[string]any{}, "", 403)
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/cancel", alice.Token, map[string]any{}, "", 200)
	request(t, server, "POST", "/api/worker/v1/runs/"+dispatched.ID+"/complete", worker.Token, map[string]any{"generation": lease.Run.Generation, "instance_id": "process", "outcome": "cancelled", "summary": "Stopped"}, "", 200)
	released := parse[store.Task](t, request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/release", alice.Token, map[string]any{}, "", 200))
	if released.ClaimedBy != nil {
		t.Fatalf("claim remained after completed cancellation: %+v", released)
	}
	request(t, server, "POST", "/api/client/v1/tasks/"+task.ID+"/release", alice.Token, map[string]any{}, "", 200)
}
