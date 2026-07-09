package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Transfer struct {
	ID        string
	DeviceID  string
	Direction Direction
	Name      string
	Size      int64
	Done      int64
	Status    TransferStatus
	Error     string
	Path      string
	FileID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	transferColumns          = "id, device_id, direction, name, size, done, status, error, path, file_id, created_at, updated_at"
	transfersQuery           = "SELECT " + transferColumns + " FROM transfers ORDER BY id DESC LIMIT ?"
	transfersOfDeviceQuery   = "SELECT " + transferColumns + " FROM transfers WHERE device_id = ? ORDER BY id DESC LIMIT ?"
	deleteFinishedQuery      = "DELETE FROM transfers WHERE status <> 'active'"
	deleteFinishedOfDevQuery = deleteFinishedQuery + " AND device_id = ?"
)

func (s *Store) InsertTransfer(ctx context.Context, t Transfer) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO transfers ("+transferColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		t.ID, t.DeviceID, string(t.Direction), t.Name, t.Size, t.Done, string(t.Status),
		nullableString(t.Error), nullableString(t.Path), nullableString(t.FileID),
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert transfer %s: %w", t.ID, err)
	}
	return nil
}

func (s *Store) Transfer(ctx context.Context, id string) (Transfer, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+transferColumns+" FROM transfers WHERE id = ?", id)
	t, err := scanTransfer(row)
	if err != nil {
		return Transfer{}, fmt.Errorf("transfer %s: %w", id, notFoundOr(err))
	}
	return t, nil
}

func (s *Store) Transfers(ctx context.Context, deviceID string, limit int) ([]Transfer, error) {
	query, args := transfersQuery, []any{limit}
	if deviceID != "" {
		query, args = transfersOfDeviceQuery, []any{deviceID, limit}
	}
	transfers, err := queryAll(ctx, s.db, scanTransfer, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list transfers of device %q: %w", deviceID, err)
	}
	return transfers, nil
}

func (s *Store) ActiveTransfers(ctx context.Context, direction Direction) ([]Transfer, error) {
	transfers, err := queryAll(ctx, s.db, scanTransfer,
		"SELECT "+transferColumns+" FROM transfers WHERE status = 'active' AND direction = ? ORDER BY id DESC",
		string(direction),
	)
	if err != nil {
		return nil, fmt.Errorf("list active %s transfers: %w", direction, err)
	}
	return transfers, nil
}

func (s *Store) ActiveTransfer(ctx context.Context, deviceID, fileID string) (Transfer, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+transferColumns+" FROM transfers WHERE status = 'active' AND device_id = ? AND file_id = ? ORDER BY id DESC LIMIT 1",
		deviceID, fileID,
	)
	t, err := scanTransfer(row)
	if err != nil {
		return Transfer{}, fmt.Errorf("active transfer of file %s to device %s: %w", fileID, deviceID, notFoundOr(err))
	}
	return t, nil
}

func (s *Store) UpdateTransfer(ctx context.Context, t Transfer) error {
	err := s.execOne(ctx,
		"UPDATE transfers SET name = ?, size = ?, done = ?, status = ?, error = ?, path = ?, file_id = ?, updated_at = ? WHERE id = ?",
		t.Name, t.Size, t.Done, string(t.Status),
		nullableString(t.Error), nullableString(t.Path), nullableString(t.FileID),
		t.UpdatedAt.UnixMilli(), t.ID,
	)
	if err != nil {
		return fmt.Errorf("update transfer %s: %w", t.ID, err)
	}
	return nil
}

func (s *Store) DeleteTransfer(ctx context.Context, id string) error {
	if err := s.execOne(ctx, "DELETE FROM transfers WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete transfer %s: %w", id, err)
	}
	return nil
}

func (s *Store) DeleteFinishedTransfers(ctx context.Context, deviceID string) error {
	query, args := deleteFinishedQuery, []any{}
	if deviceID != "" {
		query, args = deleteFinishedOfDevQuery, []any{deviceID}
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete finished transfers of device %q: %w", deviceID, err)
	}
	return nil
}

func scanTransfer(row rowScanner) (Transfer, error) {
	var (
		t         Transfer
		failure   sql.NullString
		path      sql.NullString
		fileID    sql.NullString
		createdAt int64
		updatedAt int64
	)
	err := row.Scan(&t.ID, &t.DeviceID, &t.Direction, &t.Name, &t.Size, &t.Done, &t.Status,
		&failure, &path, &fileID, &createdAt, &updatedAt)
	if err != nil {
		return Transfer{}, err
	}
	t.Error = failure.String
	t.Path = path.String
	t.FileID = fileID.String
	t.CreatedAt = timeFromMillis(createdAt)
	t.UpdatedAt = timeFromMillis(updatedAt)
	return t, nil
}
