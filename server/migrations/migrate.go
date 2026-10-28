package migrations

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 001_dispatch.sql
var initial string

// Apply serializes schema startup across control-plane instances.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(703763364621)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS pero_schema_migrations (version integer PRIMARY KEY)`); err != nil {
		return err
	}
	var applied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pero_schema_migrations WHERE version=1)`).Scan(&applied); err != nil {
		return err
	}
	if !applied {
		if _, err = tx.Exec(ctx, initial); err != nil {
			return fmt.Errorf("dispatch migration: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO pero_schema_migrations(version) VALUES(1)`); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
