package log

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"go.uber.org/zap"
	gormLogger "gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

var (
	traceStr     = "[%.3fms] [rows:%v] %s"
	traceWarnStr = "%s\t[%.3fms] [rows:%v] %s"
	traceErrStr  = "%s\t[%.3fms] [rows:%v] %s"
)

type Logger struct {
	Delegate                  *zap.Logger
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
	LogLevel                  gormLogger.LogLevel
}

var lvlMap = map[string]gormLogger.LogLevel{
	"silent": gormLogger.Silent,
	"error":  gormLogger.Error,
	"warn":   gormLogger.Warn,
	"info":   gormLogger.Info,
}

func NewGormLogger(l *zap.Logger, level string) *Logger {
	return &Logger{
		Delegate:                  l,
		SlowThreshold:             time.Second,
		IgnoreRecordNotFoundError: false,
		LogLevel:                  lvlMap[level],
	}
}

func (l *Logger) LogMode(level gormLogger.LogLevel) gormLogger.Interface {
	newLogger := *l
	newLogger.LogLevel = level
	return &newLogger
}

func (l *Logger) Info(ctx context.Context, msg string, data ...interface{}) {
	logger := l.Delegate

	fileStack := utils.FileWithLineNum()
	logger = logger.WithOptions(zap.AddCallerSkip(linesToSkip(fileStack)))

	msg = l.trimMessage(fmt.Sprintf(msg, data...))

	if l.LogLevel >= gormLogger.Error {
		logger.Info(msg)
	}
}

func (l *Logger) Warn(ctx context.Context, msg string, data ...interface{}) {
	logger := l.Delegate

	fileStack := utils.FileWithLineNum()
	logger = logger.WithOptions(zap.AddCallerSkip(linesToSkip(fileStack)))

	msg = l.trimMessage(fmt.Sprintf(msg, data...))

	if l.LogLevel >= gormLogger.Error {
		logger.Warn(msg)
	}
}

func (l *Logger) Error(ctx context.Context, msg string, data ...interface{}) {
	logger := l.Delegate

	fileStack := utils.FileWithLineNum()
	logger = logger.WithOptions(zap.AddCallerSkip(linesToSkip(fileStack)))

	msg = l.trimMessage(fmt.Sprintf(msg, data...))

	if l.LogLevel >= gormLogger.Error {
		logger.Error(msg)
	}
}

func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	logger := l.Delegate

	fileStack := utils.FileWithLineNum()
	logger = logger.WithOptions(zap.AddCallerSkip(linesToSkip(fileStack)))

	elapsed := time.Since(begin)
	sql, rows := fc()
	// trim sql
	sql = l.trimMessage(sql)

	switch {
	case err != nil && l.LogLevel >= gormLogger.Error && (!errors.Is(err, gormLogger.ErrRecordNotFound) || !l.IgnoreRecordNotFoundError):
		if rows == -1 {
			logger.Error(fmt.Sprintf(traceErrStr, err, float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			logger.Error(fmt.Sprintf(traceErrStr, err, float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	case elapsed > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= gormLogger.Warn:
		slowLog := fmt.Sprintf("SLOW SQL >= %v", l.SlowThreshold)
		if rows == -1 {
			logger.Warn(fmt.Sprintf(traceWarnStr, slowLog, float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			logger.Warn(fmt.Sprintf(traceWarnStr, slowLog, float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	case l.LogLevel == gormLogger.Info:
		if rows == -1 {
			logger.Info(fmt.Sprintf(traceStr, float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			logger.Info(fmt.Sprintf(traceStr, float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	}

	return
}

func linesToSkip(f string) int {
	// the second caller usually from gorm internal, so set i start from 2
	for i := 2; i < 17; i++ {
		_, file, line, ok := runtime.Caller(i)
		if ok && fmt.Sprintf("%s:%d", file, line) == f {
			return i - 1
		}
	}

	return 0
}

func (l *Logger) getLogger(ctx context.Context) *zap.Logger {
	logger := l.Delegate

	fileStack := utils.FileWithLineNum()
	callerSkip := zap.AddCallerSkip(linesToSkip(fileStack))

	return logger.WithOptions(callerSkip)
}

func (l *Logger) trimMessage(msg string) string {
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}

	return msg
}
