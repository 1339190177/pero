package httpapi

import (
	"github.com/phodal/pero/server/internal/store"
	"net/http"
)

func (a *API) enrollWorker(w http.ResponseWriter, r *http.Request) {
	var request store.WorkerEnrollmentRequest
	if !decode(w, r, &request) {
		return
	}
	client := &store.Client{ID: "operator", Role: "operator"}
	if r.URL.Path == "/api/client/v1/workers" {
		client = clientIdentity(r)
	}
	enrollment, err := request.Authorize(client)
	if err != nil {
		failure(w, err)
		return
	}
	worker, token, err := a.db.EnrollWorker(r.Context(), enrollment)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, map[string]any{"worker": worker, "token": token})
}

func (a *API) disableOwnedWorker(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	owner := "operator"
	if client, ok := r.Context().Value(clientContextKey{}).(*store.Client); ok {
		owner = client.ID
	}
	if err := a.db.DisableOwnedWorker(r.Context(), owner, r.PathValue("id")); err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, map[string]bool{"disabled": true})
}
