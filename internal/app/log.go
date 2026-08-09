package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	logDirName      = "logs"
	logFileName     = "ferry.log"
	rotatedLogName  = logFileName + ".1"
	maxLogFileBytes = 10 << 20
	logDirPerm      = 0o750
	logFilePerm     = 0o600
)

func openLog(dataDir string, isDev bool, stderr io.Writer) (log *slog.Logger, closeLog func() error, err error) {
	if isDev {
		log = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		return log, func() error { return nil }, nil
	}
	dir := filepath.Join(dataDir, logDirName)
	if err := os.MkdirAll(dir, logDirPerm); err != nil {
		return nil, nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, logFileName)
	rotateErr := rotateLog(path, filepath.Join(dir, rotatedLogName))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, logFilePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("open log %s: %w", path, err)
	}
	log = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if rotateErr != nil {
		log.Warn("log not rotated, appending", "err", rotateErr)
	}
	return log, f.Close, nil
}

func rotateLog(path, rotatedPath string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat log %s: %w", path, err)
	}
	if info.Size() <= maxLogFileBytes {
		return nil
	}
	if err := os.Rename(path, rotatedPath); err != nil {
		return fmt.Errorf("rotate log %s: %w", path, err)
	}
	return nil
}
