package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateTask(ctx context.Context, spec Spec, key string) (*Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	id, digest, err := receipt(ctx, tx, key, "create-task", spec)
	if err != nil {
		return nil, err
	}
	if id != "" {
		return taskIn(ctx, tx, id)
	}
	id = newID("task")
	data, _ := json.Marshal(spec)
	if _, err = tx.Exec(ctx, `INSERT INTO tasks(id,spec) VALUES($1,$2)`, id, data); err != nil {
		return nil, err
	}
	if err = saveReceipt(ctx, tx, key, "create-task", digest, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Task{ID: id, Spec: spec, State: "draft"}, nil
}

func taskIn(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (*Task, error) {
	var t Task
	var spec []byte
	err := q.QueryRow(ctx, `SELECT id,spec,COALESCE(current_run_id,'') FROM tasks WHERE id=$1`, id).Scan(&t.ID, &spec, &t.CurrentRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(spec, &t.Spec); err != nil {
		return nil, err
	}
	t.State = "draft"
	if t.CurrentRunID != "" {
		t.Run, err = readRun(q.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, t.CurrentRunID))
		if err != nil {
			return nil, err
		}
		t.State = t.Run.Status
	}
	return &t, nil
}

func (s *Store) Task(ctx context.Context, id string) (*Task, error) {
	if err := s.Reap(ctx); err != nil {
		return nil, err
	}
	return taskIn(ctx, s.pool, id)
}

func (s *Store) Tasks(ctx context.Context, cursor string, limit int) (*TaskPage, error) {
	if err := s.Reap(ctx); err != nil {
		return nil, err
	}
	var before *time.Time
	if cursor != "" {
		var value time.Time
		err := s.pool.QueryRow(ctx, `SELECT created_at FROM tasks WHERE id=$1`, cursor).Scan(&value)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		before = &value
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM tasks WHERE ($1::timestamptz IS NULL OR (created_at,id)<($1::timestamptz,$2::text)) ORDER BY created_at DESC,id DESC LIMIT $3`, before, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	page := &TaskPage{Tasks: []Task{}}
	used := 0
	for _, id := range ids {
		if len(page.Tasks) >= limit {
			page.HasMore = true
			break
		}
		t, err := taskIn(ctx, s.pool, id)
		if err != nil {
			return nil, err
		}
		// Detail owns the full Run; repeating its snapshot in every list row doubles payloads.
		t.Run = nil
		encoded, err := encodedJSON(t)
		if err != nil {
			return nil, err
		}
		if used+len(encoded) > 512*1024-4096 && len(page.Tasks) > 0 {
			page.HasMore = true
			break
		}
		used += len(encoded)
		page.Tasks = append(page.Tasks, *t)
	}
	if page.HasMore && len(page.Tasks) > 0 {
		last := page.Tasks[len(page.Tasks)-1].ID
		page.NextCursor = &last
	}
	return page, nil
}

func (s *Store) Dispatch(ctx context.Context, id, worker, key string) (*Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	operation := "dispatch:" + id
	previous, digest, err := receipt(ctx, tx, key, operation, worker)
	if err != nil {
		return nil, err
	}
	if previous != "" {
		return readRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, previous))
	}
	var spec []byte
	var current string
	err = tx.QueryRow(ctx, `SELECT spec,COALESCE(current_run_id,'') FROM tasks WHERE id=$1 FOR UPDATE`, id).Scan(&spec, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if current != "" {
		var status, target string
		if err = tx.QueryRow(ctx, `SELECT status,COALESCE(target_worker_id,'') FROM runs WHERE id=$1`, current).Scan(&status, &target); err != nil {
			return nil, err
		}
		if status == "reconcile" {
			return nil, ErrConflict
		}
		if status == "queued" || status == "running" || status == "cancel_requested" {
			if target != worker {
				return nil, ErrConflict
			}
			r, err := readRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, current))
			if err != nil {
				return nil, err
			}
			if err = saveReceipt(ctx, tx, key, operation, digest, current); err != nil {
				return nil, err
			}
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			return r, nil
		}
	}
	if worker != "" {
		var allowed bool
		err = tx.QueryRow(ctx, `SELECT ($2=ANY(projects)) FROM workers WHERE id=$1`, worker, jsonProject(spec)).Scan(&allowed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	runID := newID("run")
	r, err := readRun(tx.QueryRow(ctx, `INSERT INTO runs(id,task_id,generation,spec,target_worker_id,status) SELECT $1,$2,COALESCE(MAX(generation),0)+1,$3,NULLIF($4,''),'queued' FROM runs WHERE task_id=$2 RETURNING `+runColumns, runID, id, spec, worker))
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET current_run_id=$2 WHERE id=$1`, id, runID); err != nil {
		return nil, err
	}
	if err = saveReceipt(ctx, tx, key, operation, digest, runID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func jsonProject(data []byte) string {
	var spec Spec
	_ = json.Unmarshal(data, &spec)
	return spec.ProjectID
}

func (s *Store) Cancel(ctx context.Context, id string) (*Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var runID string
	err = tx.QueryRow(ctx, `SELECT COALESCE(current_run_id,'') FROM tasks WHERE id=$1 FOR UPDATE`, id).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if runID == "" {
		return nil, ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE runs SET status=CASE WHEN status='queued' THEN 'cancelled' WHEN lease_expires_at<=clock_timestamp() THEN 'reconcile' ELSE 'cancel_requested' END,summary=CASE WHEN status='queued' THEN 'Cancelled before execution' WHEN lease_expires_at<=clock_timestamp() THEN 'Execution lease expired; cancellation is unconfirmed' ELSE summary END WHERE id=$1 AND status IN ('queued','running','cancel_requested')`, runID)
	if err != nil {
		return nil, err
	}
	r, err := readRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, runID))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func encodedJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	err := e.Encode(value)
	return b.Bytes(), err
}

func (s *Store) Events(ctx context.Context, runID string) (*EventTail, error) {
	rows, err := s.pool.Query(ctx, `SELECT seq,kind,text,data FROM run_events WHERE run_id=$1 ORDER BY seq DESC LIMIT 201`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tail := &EventTail{Events: []Event{}}
	used := 0
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.Seq, &e.Kind, &e.Text, &e.Data); err != nil {
			return nil, err
		}
		encoded, err := encodedJSON(e)
		if err != nil {
			return nil, err
		}
		if len(tail.Events) >= 200 || used+len(encoded) > 256*1024 {
			tail.Truncated = true
			break
		}
		used += len(encoded)
		tail.Events = append(tail.Events, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(tail.Events)-1; i < j; i, j = i+1, j-1 {
		tail.Events[i], tail.Events[j] = tail.Events[j], tail.Events[i]
	}
	if len(tail.Events) > 0 {
		first, last := tail.Events[0].Seq, tail.Events[len(tail.Events)-1].Seq
		tail.FirstSeq = &first
		tail.LastSeq = &last
	}
	return tail, nil
}
