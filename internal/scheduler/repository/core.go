package repository

import (
	"context"
	"database/sql"

	"robot/internal/foundation/lockhub"
)

type SQLRepository struct {
	db       *sql.DB
	colCache map[string]map[string]bool
	locks    *lockhub.Hub
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	if db == nil {
		return nil
	}
	return &SQLRepository{
		db:       db,
		colCache: make(map[string]map[string]bool),
		locks:    lockhub.New(),
	}
}

func (r *SQLRepository) lockHub() *lockhub.Hub {
	if r.locks == nil {
		r.locks = lockhub.New()
	}
	return r.locks
}

func (r *SQLRepository) withCache(reason string, fn func()) {
	_ = r.lockHub().WithResource(lockScopeRepository, lockResourceRepositoryDDL, reason, func() error {
		fn()
		return nil
	})
}

func (r *SQLRepository) Exec(query string, args ...interface{}) (sql.Result, error) {
	return r.db.Exec(query, args...)
}

func (r *SQLRepository) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return r.db.Query(query, args...)
}

func (r *SQLRepository) QueryRow(query string, args ...interface{}) *sql.Row {
	return r.db.QueryRow(query, args...)
}

func (r *SQLRepository) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return r.db.QueryRowContext(ctx, query, args...)
}

func (r *SQLRepository) Begin() (*sql.Tx, error) {
	return r.db.Begin()
}

func (r *SQLRepository) PingContext(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *SQLRepository) Stats() sql.DBStats {
	return r.db.Stats()
}
