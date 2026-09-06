// Package applog provides leveled file logging for the application itself
// (trace/debug/info/warn/error), separate from the easytier-core child log.
// Entries are appended to <dir>/app-YYYY-MM-DD.log (one file per day) with a
// timestamp, level and component prefix. Level filtering is global and set
// from settings; everything below the threshold is discarded.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level of a log entry, ordered by severity.
type Level int

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = [...]string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR"}

// String returns the uppercase level name.
func (l Level) String() string { return levelNames[l] }

// ParseLevel maps a settings string ("debug", "info", …) to a Level.
// Unknown/empty values default to Info.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger writes leveled entries to daily files under a directory. Safe for
// concurrent use.
type Logger struct {
	mu      sync.Mutex
	dir     string
	minimum Level
	day     string // "2006-01-02" of the open file
	f       *os.File
}

// New creates a logger rooted at dir (created lazily) with the given minimum
// level.
func New(dir string, min Level) *Logger {
	return &Logger{dir: dir, minimum: min}
}

// SetLevel adjusts the threshold at runtime (settings change).
func (l *Logger) SetLevel(min Level) {
	l.mu.Lock()
	l.minimum = min
	l.mu.Unlock()
}

// Level reports the current threshold.
func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.minimum
}

// Dir returns the log directory.
func (l *Logger) Dir() string { return l.dir }

// Log writes one entry at the given level. format follows fmt.Sprintf.
// No-op when level is below the threshold.
func (l *Logger) Log(level Level, component, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if level < l.minimum {
		return
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	if l.f == nil || l.day != day {
		l.rotateLocked(day)
	}
	if l.f == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s [%s] %s\n",
		now.Format("2006-01-02 15:04:05.000"), level.String(), component, msg)
	_, _ = l.f.WriteString(line)
}

// rotateLocked closes the current file and opens the day's file.
// Caller holds mu.
func (l *Logger) rotateLocked(day string) {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(l.dir, "app-"+day+".log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	l.f = f
	l.day = day
}

// Close flushes and closes the current file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

// Tail returns the newest lines from the logger's files (today first, then
// previous days) up to limit. Used by the settings log viewer.
func Tail(l *Logger, limit int) []string {
	if limit <= 0 {
		limit = 200
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return []string{}
	}
	// Newest file last in lexical order (date-named); walk backwards.
	var out []string
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		e := entries[i]
		if e.IsDir() || !strings.HasPrefix(e.Name(), "app-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(l.dir, e.Name()))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for j := len(lines) - 1; j >= 0 && len(out) < limit; j-- {
			out = append(out, lines[j])
		}
	}
	// Collected newest-first; reverse so the caller can render chronologically.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Convenience helpers. Components mirror the emitting area ("web", "core",
// "devices", "quota", "app").

func (l *Logger) Tracef(component, format string, args ...any) {
	l.Log(LevelTrace, component, format, args...)
}

func (l *Logger) Debugf(component, format string, args ...any) {
	l.Log(LevelDebug, component, format, args...)
}

func (l *Logger) Infof(component, format string, args ...any) {
	l.Log(LevelInfo, component, format, args...)
}

func (l *Logger) Warnf(component, format string, args ...any) {
	l.Log(LevelWarn, component, format, args...)
}

func (l *Logger) Errorf(component, format string, args ...any) {
	l.Log(LevelError, component, format, args...)
}
