package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Current source positions order the document; frozen goals retain their provenance.
// The cursor must belong to the same visible source/tag set as the next page.
const roadmapCandidates = `SELECT tasks.id,
 COALESCE(road.latest->>'repository',spec->'source'->>'repository','') AS repository,
 COALESCE(road.latest->>'path',spec->'source'->>'path','') AS path,
 COALESCE((road.latest->>'line')::int,(spec->'source'->>'line')::int,0) AS line
 FROM tasks LEFT JOIN roadmap_items road ON road.task_id=tasks.id
 WHERE (NOT $1::boolean OR spec->>'project_id'=ANY($2::text[]))
 AND spec->'source'->>'kind'='roadmap'
 AND ($3='' OR (CASE WHEN road.task_id IS NOT NULL THEN COALESCE(road.latest->'tags','[]'::jsonb)
 ELSE COALESCE(spec->'tags','[]'::jsonb) END) ? $3)`

func (s *Store) roadmapTaskIDs(ctx context.Context, client *Client, cursor string, limit int, filter TaskFilter) ([]string, error) {
	projects := []string{}
	if client != nil {
		projects = client.Projects
	}
	var line *int
	var repository, path string
	if cursor != "" {
		var value int
		err := s.pool.QueryRow(ctx, `WITH matching AS (`+roadmapCandidates+`) SELECT repository,path,line FROM matching WHERE id=$4`, client != nil, projects, filter.Tag, cursor).Scan(&repository, &path, &value)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		line = &value
	}
	rows, err := s.pool.Query(ctx, `WITH matching AS (`+roadmapCandidates+`) SELECT id FROM matching
 WHERE ($4::int IS NULL OR (repository,path,line,id)>($5::text,$6::text,$4::int,$7::text))
 ORDER BY repository,path,line,id LIMIT $8`, client != nil, projects, filter.Tag, line, repository, path, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
