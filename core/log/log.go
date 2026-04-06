package log

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type Field struct {
	Key   string
	Value any
}

type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

var (
	loggerMu sync.RWMutex
	logger   Logger = &fallbackLogger{}
)

func SetLogger(next Logger) Logger {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	previous := logger
	if next == nil {
		logger = &fallbackLogger{}
	} else {
		logger = next
	}
	return previous
}

func currentLogger() Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	if logger == nil {
		return &fallbackLogger{}
	}
	return logger
}

func Debug(msg string, fields ...Field) { currentLogger().Debug(msg, fields...) }
func Info(msg string, fields ...Field)  { currentLogger().Info(msg, fields...) }
func Warn(msg string, fields ...Field)  { currentLogger().Warn(msg, fields...) }
func Error(msg string, fields ...Field) { currentLogger().Error(msg, fields...) }

func Debugf(format string, args ...any) { Debug(fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { Error(fmt.Sprintf(format, args...)) }

func String(key string, value string) Field            { return Field{Key: key, Value: value} }
func Int(key string, value int) Field                  { return Field{Key: key, Value: value} }
func Int64(key string, value int64) Field              { return Field{Key: key, Value: value} }
func Bool(key string, value bool) Field                { return Field{Key: key, Value: value} }
func Duration(key string, value time.Duration) Field   { return Field{Key: key, Value: value} }
func Any(key string, value any) Field                  { return Field{Key: key, Value: value} }
func Err(err error) Field                              { return Field{Key: "error", Value: err} }

type fallbackLogger struct{}

func (l *fallbackLogger) Debug(msg string, fields ...Field) { writeFallback("DEBUG", msg, fields...) }
func (l *fallbackLogger) Info(msg string, fields ...Field)  { writeFallback("INFO", msg, fields...) }
func (l *fallbackLogger) Warn(msg string, fields ...Field)  { writeFallback("WARN", msg, fields...) }
func (l *fallbackLogger) Error(msg string, fields ...Field) { writeFallback("ERROR", msg, fields...) }

func writeFallback(level string, msg string, fields ...Field) {
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(time.Now().UTC().Format("2006-01-02 15:04:05.000"))
	builder.WriteString("] ")
	builder.WriteString(level)
	builder.WriteString(" ")
	builder.WriteString(msg)

	fieldParts := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Key) == "" {
			continue
		}
		fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", field.Key, field.Value))
	}
	if len(fieldParts) > 0 {
		builder.WriteString(" | ")
		builder.WriteString(strings.Join(fieldParts, " "))
	}
	_, _ = fmt.Fprintln(os.Stdout, builder.String())
}
