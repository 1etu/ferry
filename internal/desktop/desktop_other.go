//go:build !windows

package desktop

import "log/slog"

type Window struct{}

func New(_, _, _ string, _ *slog.Logger) *Window {
	return &Window{}
}

func (w *Window) Show() {}

func (w *Window) Close() {}

func (w *Window) Available() bool {
	return false
}
