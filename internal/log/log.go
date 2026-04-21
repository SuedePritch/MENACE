package log

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	file    *os.File
	logger  *slog.Logger
	initDone bool
)

func Init(menaceDir string) {
	path := filepath.Join(menaceDir, "menace.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	file = f
	// slog.JSONHandler is safe for concurrent use — no external mutex needed.
	logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	initDone = true
	Info("MENACE started")
}

func Close() {
	if file != nil {
		file.Close()
	}
}

func Info(msg string, attrs ...slog.Attr) {
	if !initDone {
		return
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, msg, attrs...)
}

func Error(msg string, attrs ...slog.Attr) {
	if !initDone {
		return
	}
	logger.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
}

func Debug(msg string, attrs ...slog.Attr) {
	if !initDone {
		return
	}
	logger.LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
}
