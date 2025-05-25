package main

import (
	"fmt"
	"github.com/csmith/slogflags"
	"github.com/go-acme/lego/v4/log"
	"log/slog"
	"regexp"
	"strings"
)

func initLogging() {
	_ = slogflags.Logger(
		slogflags.WithOldLogLevel(slog.LevelDebug),
		slogflags.WithSetDefault(true),
	)
}

// legoLogger implements the log.StdLogger interface used by lego, and mangles
// its log messages into somewhat more structured logs.
type legoLogger struct {
	logger *slog.Logger
}

func (l *legoLogger) Fatal(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}

func (l *legoLogger) Fatalln(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}

func (l *legoLogger) Fatalf(format string, args ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

var domainRegex = regexp.MustCompile(`^\[[a-zA-Z0-9-.]+] `)

func (l *legoLogger) Print(args ...interface{}) {
	message := fmt.Sprint(args...)
	fn := l.logger.Debug
	if strings.HasPrefix(message, "[WARN] ") {
		message = strings.TrimPrefix(message, "[WARN] ")
		fn = l.logger.Warn
	} else if strings.HasPrefix(message, "[INFO] ") {
		message = strings.TrimPrefix(message, "[INFO] ")
		fn = l.logger.Info
	}

	var ourArgs []any
	if domainRegex.MatchString(message) {
		prefix := domainRegex.FindString(message)
		domain := strings.Trim(prefix, " []")
		ourArgs = append(ourArgs, "domain", domain)
		message = strings.TrimPrefix(message, prefix)
	}
	fn(message, ourArgs...)
}

func (l *legoLogger) Println(args ...interface{}) {
	l.Print(args...)
}

func (l *legoLogger) Printf(format string, args ...interface{}) {
	l.Print(fmt.Sprintf(format, args...))
}

func init() {
	log.Logger = &legoLogger{
		logger: slog.With("component", "lego"),
	}
}
