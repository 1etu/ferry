package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Device struct {
	ID         string
	Name       string
	TokenHash  []byte
	Status     DeviceStatus
	Secret     []byte
	CreatedAt  time.Time
	ApprovedAt time.Time
	LastSeenAt time.Time
}

const deviceColumns = "id, name, token_hash, status, secret, created_at, approved_at, last_seen_at"

func (s *Store) InsertDevice(ctx context.Context, d Device) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO devices ("+deviceColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		d.ID, d.Name, d.TokenHash, string(d.Status), nullableBytes(d.Secret), d.CreatedAt.UnixMilli(),
		nullableMillis(d.ApprovedAt), nullableMillis(d.LastSeenAt),
	)
	if err != nil {
		return fmt.Errorf("insert device %s: %w", d.ID, err)
	}
	return nil
}

func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id = ?", id)
	d, err := scanDevice(row)
	if err != nil {
		return Device{}, fmt.Errorf("device %s: %w", id, notFoundOr(err))
	}
	return d, nil
}

func (s *Store) DeviceByTokenHash(ctx context.Context, hash []byte) (Device, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE token_hash = ?", hash)
	d, err := scanDevice(row)
	if err != nil {
		return Device{}, fmt.Errorf("device by token hash: %w", notFoundOr(err))
	}
	return d, nil
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	devices, err := queryAll(ctx, s.db, scanDevice, "SELECT "+deviceColumns+" FROM devices ORDER BY id DESC")
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

func (s *Store) SetDeviceStatus(ctx context.Context, id string, status DeviceStatus, at time.Time) error {
	err := s.execOne(ctx,
		"UPDATE devices SET status = ?, approved_at = CASE WHEN ? THEN ? ELSE approved_at END WHERE id = ?",
		string(status), status == DeviceApproved, at.UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("set device %s status %s: %w", id, status, err)
	}
	return nil
}

func (s *Store) TouchDevice(ctx context.Context, id string, at time.Time) error {
	if err := s.execOne(ctx, "UPDATE devices SET last_seen_at = ? WHERE id = ?", at.UnixMilli(), id); err != nil {
		return fmt.Errorf("touch device %s: %w", id, err)
	}
	return nil
}

func (s *Store) SetDeviceSecret(ctx context.Context, id string, secret []byte) error {
	if err := s.execOne(ctx, "UPDATE devices SET secret = ? WHERE id = ?", nullableBytes(secret), id); err != nil {
		return fmt.Errorf("set device %s secret: %w", id, err)
	}
	return nil
}

func scanDevice(row rowScanner) (Device, error) {
	var (
		d          Device
		createdAt  int64
		approvedAt sql.NullInt64
		lastSeenAt sql.NullInt64
	)
	if err := row.Scan(&d.ID, &d.Name, &d.TokenHash, &d.Status, &d.Secret, &createdAt, &approvedAt, &lastSeenAt); err != nil {
		return Device{}, err
	}
	d.CreatedAt = timeFromMillis(createdAt)
	d.ApprovedAt = timeFromNullableMillis(approvedAt)
	d.LastSeenAt = timeFromNullableMillis(lastSeenAt)
	return d, nil
}
