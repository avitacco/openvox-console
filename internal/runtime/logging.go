package runtime

import (
	"io"
	"log/slog"
)

// NewLogger returns a structured (JSON) logger writing to w.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}
