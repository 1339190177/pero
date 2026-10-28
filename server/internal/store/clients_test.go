package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func mustClient(t *testing.T, s *Store, name string, projects ...string) (Client, string) {
	t.Helper()
	client, token, err := s.CreateClient(context.Background(), name, projects)
	if err != nil {
		t.Fatal(err)
	}
	return client, token
}

func TestClientProjectScopeAndAtomicHumanClaims(t *testing.T) {
	s, dsn := integrationStore(t)
	ctx := context.Background()
	a, token := mustClient(t, s, "Alice", "shared")
	b, _ := mustClient(t, s, "Bob", "shared")
	identity, err := s.AuthenticateClient(ctx, token)
	if err != nil || identity.ID != a.ID || identity.Role != "client" {
		t.Fatalf("client identity: %+v %v", identity, err)
	}
	if _, err = s.AuthenticateWorker(ctx, token); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client token became worker: %v", err)
	}
	_, workerToken := mustWorker(t, s, "shared")
	if _, err = s.AuthenticateClient(ctx, workerToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("worker token became client: %v", err)
	}
	public, err := s.CreateTask(ctx, testSpec("shared"), "")
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateTask(ctx, testSpec("private"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClientTask(ctx, &a, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private detail: %v", err)
	}
	if _, err = s.ClientTasks(ctx, &a, private.ID, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private cursor: %v", err)
	}
	page, err := s.ClientTasks(ctx, &a, "", 50)
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].ID != public.ID {
		t.Fatalf("scoped list: %+v %v", page, err)
	}
	var wg sync.WaitGroup
	winners := make(chan string, 12)
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		client := a
		if i%2 != 0 {
			client = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := s.ClaimTask(ctx, &client, public.ID)
			if err != nil {
				failures <- err
				return
			}
			winners <- task.ClaimedBy.ID
		}()
	}
	wg.Wait()
	close(winners)
	close(failures)
	owner := ""
	for id := range winners {
		if owner != "" && owner != id {
			t.Fatalf("two human owners: %s %s", owner, id)
		}
		owner = id
	}
	if owner == "" {
		t.Fatal("no claim winner")
	}
	for err := range failures {
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("claim race error: %v", err)
		}
	}
	winner, loser := a, b
	if owner == b.ID {
		winner, loser = b, a
	}
	if _, err = s.ReleaseTask(ctx, &loser, public.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner release: %v", err)
	}
	if _, err = s.ClientDispatch(ctx, &loser, public.ID, "", "shared-key"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner dispatch: %v", err)
	}
	foreign, _ := mustWorker(t, s, "private")
	if _, err = s.ClientDispatch(ctx, &winner, public.ID, foreign.ID, "foreign"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign worker dispatch: %v", err)
	}
	run, err := s.ClientDispatch(ctx, &winner, public.ID, "", "shared-key")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.ClientDispatch(ctx, &winner, public.ID, "", "shared-key")
	if err != nil || repeated.ID != run.ID {
		t.Fatalf("dispatch receipt: %+v %v", repeated, err)
	}
	if _, err = s.ReleaseTask(ctx, &winner, public.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("human release revoked live execution: %v", err)
	}
	if _, err = s.ClientCancel(ctx, &loser, public.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner cancel: %v", err)
	}
	if cancelled, err := s.ClientCancel(ctx, &winner, public.ID); err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("owner cancel: %+v %v", cancelled, err)
	}
	if released, err := s.ReleaseTask(ctx, &winner, public.ID); err != nil || released.ClaimedBy != nil {
		t.Fatalf("release: %+v %v", released, err)
	}
	// The same caller-provided key belongs to each authenticated identity separately.
	other, err := s.CreateTask(ctx, testSpec("shared"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTask(ctx, &loser, other.ID); err != nil {
		t.Fatal(err)
	}
	otherRun, err := s.ClientDispatch(ctx, &loser, other.ID, "", "shared-key")
	if err != nil || otherRun.ID == run.ID {
		t.Fatalf("actor-scoped key: %+v %v", otherRun, err)
	}
	// A private Task is rejected before even looking up this otherwise valid receipt key.
	if _, err = s.ClientDispatch(ctx, &winner, private.ID, "", "shared-key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private receipt leaked: %v", err)
	}
	reopened, err := Open(ctx, dsn, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	durable, err := reopened.Task(ctx, other.ID)
	if err != nil || durable.ClaimedBy == nil || durable.ClaimedBy.ID != loser.ID {
		t.Fatalf("durable human claim: %+v %v", durable, err)
	}
}

func TestClientSourceClosedAndWorkerVisibility(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	client, _ := mustClient(t, s, "Member", "shared")
	task, err := s.CreateTask(ctx, testSpec("shared"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE tasks SET spec=jsonb_set(spec,'{source}','{"kind":"github","state":"closed"}'::jsonb) WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTask(ctx, &client, task.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed source claim: %v", err)
	}
	if _, err = s.Dispatch(ctx, task.ID, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed source operator dispatch: %v", err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE tasks SET spec=jsonb_set(spec,'{source,state}','"open"'::jsonb) WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTask(ctx, &client, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE tasks SET spec=jsonb_set(spec,'{source,state}','"closed"'::jsonb) WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClientDispatch(ctx, &client, task.ID, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed source client dispatch: %v", err)
	}
	if _, _, err = s.CreateWorker(ctx, "Shared", []string{"shared", "hidden"}, nil); err != nil {
		t.Fatal(err)
	}
	mustWorker(t, s, "private")
	workers, err := s.ClientWorkers(ctx, &client)
	if err != nil || len(workers) != 1 || len(workers[0].Projects) != 1 || workers[0].Projects[0] != "shared" {
		t.Fatalf("worker visibility: %+v %v", workers, err)
	}
}
