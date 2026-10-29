package httpapi_test

import (
	"github.com/phodal/pero/server/internal/store"
	"strings"
	"testing"
)

func TestDesktopWorkerEnrollmentAndOwnership(t *testing.T) {
	api := testAPI(t)
	type clientCredentials struct {
		Client store.Client `json:"client"`
		Token  string       `json:"token"`
	}
	alice := parse[clientCredentials](t, request(t, api, "POST", "/api/v1/clients", "test-operator", map[string]any{"name": "Alice", "projects": []string{"team"}}, "", 201))
	bob := parse[clientCredentials](t, request(t, api, "POST", "/api/v1/clients", "test-operator", map[string]any{"name": "Bob", "projects": []string{"team"}}, "", 201))
	input := store.WorkerEnrollmentRequest{DeviceID: "device", RequestID: "request-1", Name: "Alice laptop", Projects: []string{"team"}, Capabilities: []store.WorkerCapability{{ProjectID: "team", WorkspaceKey: "repo", AgentID: "fixture"}}, EnrollmentSecret: strings.Repeat("ab", 32)}
	type receipt struct {
		Worker store.Worker `json:"worker"`
		Token  string       `json:"token"`
	}
	created := parse[receipt](t, request(t, api, "POST", "/api/client/v1/workers", alice.Token, input, "", 200))
	recovered := parse[receipt](t, request(t, api, "POST", "/api/client/v1/workers", alice.Token, input, "", 200))
	if created.Worker.ID != recovered.Worker.ID || created.Token != recovered.Token {
		t.Fatal("enrollment replay duplicated device/credential")
	}
	request(t, api, "POST", "/api/client/v1/workers/"+created.Worker.ID+"/disable", bob.Token, map[string]any{}, "", 404)
	invalid := input
	invalid.Capabilities = nil
	request(t, api, "POST", "/api/client/v1/workers", alice.Token, invalid, "", 400)
	foreign := input
	foreign.Projects = []string{"private"}
	foreign.Capabilities = []store.WorkerCapability{{ProjectID: "private", WorkspaceKey: "repo", AgentID: "fixture"}}
	request(t, api, "POST", "/api/client/v1/workers", alice.Token, foreign, "", 403)
	request(t, api, "POST", "/api/worker/v1/claims", alice.Token, map[string]any{"request_id": "claim", "instance_id": "instance"}, "", 401)
	request(t, api, "POST", "/api/client/v1/workers/"+created.Worker.ID+"/disable", alice.Token, map[string]any{}, "", 200)
	request(t, api, "POST", "/api/worker/v1/claims", created.Token, map[string]any{"request_id": "claim", "instance_id": "instance"}, "", 401)
	input.RequestID = "operator-enroll"
	request(t, api, "POST", "/api/v1/workers/enroll", "test-operator", input, "", 200)
}
