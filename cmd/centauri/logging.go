package main

import (
	"log/slog"

	"github.com/csmith/slogflags"
	"github.com/go-acme/lego/v5/log"
)

func initLogging() {
	logger := slogflags.Logger(
		slogflags.WithOldLogLevel(slog.LevelDebug),
		slogflags.WithSetDefault(true),
	)
	log.SetDefault(logger.With("component", "lego"))
}
