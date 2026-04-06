package logging

import (
	corelog "github.com/EquentR/agent_runtime/core/log"
	"go.uber.org/zap"
)

type CoreAdapter struct {
	logger *zap.Logger
}

func NewCoreAdapter(logger *zap.Logger) *CoreAdapter {
	if logger == nil {
		return &CoreAdapter{}
	}
	return &CoreAdapter{logger: logger}
}

func (a *CoreAdapter) Debug(msg string, fields ...corelog.Field) { a.log(func(l *zap.Logger, fs ...zap.Field) { l.Debug(msg, fs...) }, fields...) }
func (a *CoreAdapter) Info(msg string, fields ...corelog.Field)  { a.log(func(l *zap.Logger, fs ...zap.Field) { l.Info(msg, fs...) }, fields...) }
func (a *CoreAdapter) Warn(msg string, fields ...corelog.Field)  { a.log(func(l *zap.Logger, fs ...zap.Field) { l.Warn(msg, fs...) }, fields...) }
func (a *CoreAdapter) Error(msg string, fields ...corelog.Field) { a.log(func(l *zap.Logger, fs ...zap.Field) { l.Error(msg, fs...) }, fields...) }

func (a *CoreAdapter) log(write func(*zap.Logger, ...zap.Field), fields ...corelog.Field) {
	if a == nil || a.logger == nil {
		return
	}
	zapFields := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		if field.Key == "" {
			continue
		}
		switch value := field.Value.(type) {
		case string:
			zapFields = append(zapFields, zap.String(field.Key, value))
		case int:
			zapFields = append(zapFields, zap.Int(field.Key, value))
		case int64:
			zapFields = append(zapFields, zap.Int64(field.Key, value))
		case bool:
			zapFields = append(zapFields, zap.Bool(field.Key, value))
		case error:
			zapFields = append(zapFields, zap.NamedError(field.Key, value))
		default:
			zapFields = append(zapFields, zap.Any(field.Key, value))
		}
	}
	write(a.logger, zapFields...)
}
