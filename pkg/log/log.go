package log

import "go.uber.org/zap"

func Debug(msg string, fields ...zap.Field) {
	Log().Debug(msg, fields...)
}

func Debugf(template string, args ...interface{}) {
	Log().Sugar().Debugf(template, args...)
}

func Info(msg string, fields ...zap.Field) {
	Log().Info(msg, fields...)
}

func Infof(template string, args ...interface{}) {
	Log().Sugar().Infof(template, args...)
}

func Warn(msg string, fields ...zap.Field) {
	Log().Warn(msg, fields...)
}

func Warnf(template string, args ...interface{}) {
	Log().Sugar().Warnf(template, args...)
}

func Error(msg string, fields ...zap.Field) {
	Log().Error(msg, fields...)
}

func Errorf(template string, args ...interface{}) {
	Log().Sugar().Errorf(template, args...)
}

func DPanic(msg string, fields ...zap.Field) {
	Log().DPanic(msg, fields...)
}

func DPanicf(template string, args ...interface{}) {
	Log().Sugar().DPanicf(template, args...)
}

func Panic(msg string, fields ...zap.Field) {
	Log().Panic(msg, fields...)
}

func Panicf(template string, args ...interface{}) {
	Log().Sugar().Panicf(template, args...)
}

func Fatal(msg string, fields ...zap.Field) {
	Log().Fatal(msg, fields...)
}

func Fatalf(template string, args ...interface{}) {
	Log().Sugar().Fatalf(template, args...)
}
