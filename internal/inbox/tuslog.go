package inbox

import (
	"context"
	"log/slog"
	"slices"

	xslog "golang.org/x/exp/slog"
)

type tusLogHandler struct {
	target slog.Handler
	attrs  []slog.Attr
}

func tusLogger(log *slog.Logger) *xslog.Logger {
	return xslog.New(tusLogHandler{target: log.Handler()})
}

func (h tusLogHandler) Enabled(ctx context.Context, level xslog.Level) bool {
	return h.target.Enabled(ctx, levelOf(level))
}

func (h tusLogHandler) Handle(ctx context.Context, r xslog.Record) error {
	record := slog.NewRecord(r.Time, levelOf(r.Level), r.Message, r.PC)
	record.AddAttrs(h.attrs...)
	r.Attrs(func(a xslog.Attr) bool {
		record.AddAttrs(slog.Any(a.Key, a.Value.Any()))
		return true
	})
	return h.target.Handle(ctx, record)
}

func (h tusLogHandler) WithAttrs(attrs []xslog.Attr) xslog.Handler {
	merged := slices.Clone(h.attrs)
	for _, a := range attrs {
		merged = append(merged, slog.Any(a.Key, a.Value.Any()))
	}
	return tusLogHandler{target: h.target, attrs: merged}
}

func (h tusLogHandler) WithGroup(name string) xslog.Handler {
	return tusLogHandler{target: h.target.WithGroup(name), attrs: h.attrs}
}

func levelOf(level xslog.Level) slog.Level {
	if level >= xslog.LevelWarn {
		return slog.LevelWarn
	}
	return slog.LevelDebug
}
