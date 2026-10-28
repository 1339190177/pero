package httpapi

import (
	"net/http"

	"github.com/phodal/pero/server/internal/github"
	"github.com/phodal/pero/server/internal/store"
)

func (a *API) registerGitHub(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/client/v1/github/status", func(w http.ResponseWriter, r *http.Request) {
		if a.github == nil {
			respond(w, 200, github.Status{Repositories: []github.RepositoryStatus{}, Errors: []string{}})
			return
		}
		respond(w, 200, a.github.StatusForProjects(clientIdentity(r).Projects))
	})
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

func taskFilter(w http.ResponseWriter, r *http.Request) (store.TaskFilter, bool) {
	f := store.TaskFilter{SourceKind: r.URL.Query().Get("source_kind"), Tag: r.URL.Query().Get("tag")}
	if f.SourceKind != "" && f.SourceKind != "github" && f.SourceKind != "roadmap" || f.Tag != "" && !bounded(f.Tag, 128) {
		invalid(w)
		return f, false
	}
	return f, true
}
