package store

import (
	"context"
	"fmt"
	"time"
)

type File struct {
	ID        string
	Path      string
	Name      string
	Size      int64
	ModTime   time.Time
	CreatedAt time.Time
}

const fileColumns = "id, path, name, size, mod_time, created_at"

func (s *Store) InsertFile(ctx context.Context, f File) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO files ("+fileColumns+") VALUES (?, ?, ?, ?, ?, ?)",
		f.ID, f.Path, f.Name, f.Size, f.ModTime.UnixMilli(), f.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert file %s: %w", f.ID, err)
	}
	return nil
}

func (s *Store) File(ctx context.Context, id string) (File, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+fileColumns+" FROM files WHERE id = ?", id)
	f, err := scanFile(row)
	if err != nil {
		return File{}, fmt.Errorf("file %s: %w", id, notFoundOr(err))
	}
	return f, nil
}

func (s *Store) Files(ctx context.Context) ([]File, error) {
	files, err := queryAll(ctx, s.db, scanFile, "SELECT "+fileColumns+" FROM files ORDER BY id DESC")
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

func (s *Store) UpdateFile(ctx context.Context, f File) error {
	err := s.execOne(ctx,
		"UPDATE files SET path = ?, name = ?, size = ?, mod_time = ? WHERE id = ?",
		f.Path, f.Name, f.Size, f.ModTime.UnixMilli(), f.ID,
	)
	if err != nil {
		return fmt.Errorf("update file %s: %w", f.ID, err)
	}
	return nil
}

func (s *Store) DeleteFile(ctx context.Context, id string) error {
	if err := s.execOne(ctx, "DELETE FROM files WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete file %s: %w", id, err)
	}
	return nil
}

func (s *Store) DeleteFilesBefore(ctx context.Context, t time.Time) ([]File, error) {
	files, err := queryAll(ctx, s.db, scanFile,
		"DELETE FROM files WHERE created_at < ? RETURNING "+fileColumns,
		t.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("delete files created before %s: %w", t.Format(time.RFC3339), err)
	}
	return files, nil
}

func scanFile(row rowScanner) (File, error) {
	var (
		f         File
		modTime   int64
		createdAt int64
	)
	if err := row.Scan(&f.ID, &f.Path, &f.Name, &f.Size, &modTime, &createdAt); err != nil {
		return File{}, err
	}
	f.ModTime = timeFromMillis(modTime)
	f.CreatedAt = timeFromMillis(createdAt)
	return f, nil
}
