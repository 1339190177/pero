package httpapi

import (
	"net/http"

	"github.com/phodal/pero/server/internal/github"
)

func (a *API) registerGitHub(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/github/status", func(w http.ResponseWriter, r *http.Request) {
		if a.github == nil {
			respond(w, 200, github.Status{Repositories: []github.RepositoryStatus{}, Errors: []string{}})
			return
		}
		respond(w, 200, a.github.Status())
	})
	mux.HandleFunc("POST /api/v1/github/sync", func(w http.ResponseWriter, r *http.Request) {
		var empty struct{}
		if !decode(w, r, &empty) {
			return
		}
		if a.github == nil {
			respond(w, 200, github.Result{Errors: []string{}})
			return
		}
		result, err := a.github.Sync(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, result)
	})
}
