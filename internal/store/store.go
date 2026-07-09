package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const schemaVersion = 2

var ErrNotFound = errors.New("not found")

type DeviceStatus string

const (
	DevicePending  DeviceStatus = "pending"
	DeviceApproved DeviceStatus = "approved"
	DeviceRevoked  DeviceStatus = "revoked"
)

type Direction string

const (
	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

type TransferStatus string

const (
	TransferActive   TransferStatus = "active"
	TransferDone     TransferStatus = "done"
	TransferFailed   TransferStatus = "failed"
	TransferCanceled TransferStatus = "canceled"
)

type Store struct{ db *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("migrate database %s: %w", path, err), db.Close())
	}
	return s, nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

func dataSourceName(path string) string {
	escapeURIPath := strings.NewReplacer("%", "%25", "#", "%23", "?", "%3f")
	return "file:" + escapeURIPath.Replace(filepath.ToSlash(path)) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	if err := applySchema(ctx, tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}
	return nil
}

func applySchema(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	switch version {
	case 0:
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		return nil
	case 1:
		if _, err := tx.ExecContext(ctx, migrationFrom1); err != nil {
			return fmt.Errorf("migrate schema from version 1: %w", err)
		}
		return nil
	case schemaVersion:
		return nil
	default:
		return fmt.Errorf("unsupported schema version %d, this build supports up to %d", version, schemaVersion)
	}
}

const migrationFrom1 = "ALTER TABLE devices ADD COLUMN secret BLOB; PRAGMA user_version = 2;"

func (s *Store) execOne(ctx context.Context, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func notFoundOr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func timeFromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

func nullableMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func timeFromNullableMillis(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return timeFromMillis(ms.Int64)
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func queryAll[T any](ctx context.Context, db *sql.DB, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := make([]T, 0)
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return all, nil
}
