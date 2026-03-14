package log

import (
	"errors"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level      string `yaml:"level"`
	File       string `yaml:"file"`
	Rotation   bool   `yaml:"rotation"`
	MaxSize    int    `yaml:"maxSize"`
	MaxAge     int    `yaml:"maxAge"`
	MaxBackups int    `yaml:"maxBackups"`
	Compress   bool   `yaml:"compress"`
}

var (
	logger *zap.Logger
)

func NewZapConfig() *zap.Config {
	var c zap.Config
	if debug, ok := os.LookupEnv("DEBUG"); ok && debug == "true" {
		c = zap.NewDevelopmentConfig()
	} else {
		c = zap.NewProductionConfig()
	}
	return &c
}

func NewLumberjackConfigDefault() *lumberjack.Logger {
	return &lumberjack.Logger{
		MaxSize:    1024,
		MaxAge:     7,
		MaxBackups: 3,
		LocalTime:  true,
		Compress:   true,
	}
}

func NewZapLoggerWithConf(config *zap.Config, lumber *lumberjack.Logger, opts ...zap.Option) (*zap.Logger, error) {
	if config == nil {
		return nil, errors.New("zap config is nil")
	}

	// ----------- EncoderConfig 优化（console 格式）-----------
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder // 两边统一格式
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	consoleEncoder := zapcore.NewConsoleEncoder(config.EncoderConfig)

	// ----------- JSON encoder（文件）-----------
	jsonEncoder := zapcore.NewJSONEncoder(config.EncoderConfig)

	// ----------- stdout/stderr syncers -----------
	var consoleSyncers []zapcore.WriteSyncer
	for _, p := range config.OutputPaths {
		if p == "stdout" || p == "stderr" {
			ws, closeFn, err := zap.Open(p)
			if err != nil {
				closeFn()
				continue
			}
			consoleSyncers = append(consoleSyncers, ws)
		}
	}

	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zap.CombineWriteSyncers(consoleSyncers...),
		config.Level,
	)

	// ----------- file syncers -----------
	var fileSyncers []zapcore.WriteSyncer
	for _, p := range config.OutputPaths {
		if p != "stdout" && p != "stderr" {
			if lumber != nil {
				fileSyncers = append(fileSyncers,
					zapcore.AddSync(&lumberjack.Logger{
						Filename:   p,
						MaxAge:     lumber.MaxAge,
						MaxBackups: lumber.MaxBackups,
						MaxSize:    lumber.MaxSize,
						Compress:   lumber.Compress,
						LocalTime:  lumber.LocalTime,
					}),
				)
			} else {
				ws, closeFn, err := zap.Open(p)
				if err != nil {
					closeFn()
					continue
				}
				fileSyncers = append(fileSyncers, ws)
			}
		}
	}

	fileCore := zapcore.NewCore(
		jsonEncoder,
		zap.CombineWriteSyncers(fileSyncers...),
		config.Level,
	)

	// ----------- 合并 core + 强制 caller -----------
	core := zapcore.NewTee(consoleCore, fileCore)

	// caller + 统一跳过层级
	opts = append(opts, zap.AddCaller())

	return zap.New(core, opts...), nil
}

func NewLogger(l *Config) *zap.Logger {
	if l == nil {
		panic("log config must not be nil")
	}
	var err error
	zc := NewZapConfig()
	if len(zc.OutputPaths) > 0 {
		for i, p := range zc.OutputPaths {
			if p == "stderr" {
				zc.OutputPaths[i] = "stdout"
			}
		}
	}
	level, err := ParseZapLevel(l.Level)
	if err != nil {
		panic(err)
	}
	zc.Level = level
	if l.File != "" {
		zc.OutputPaths = append(zc.OutputPaths, l.File)
		zc.ErrorOutputPaths = append(zc.ErrorOutputPaths, l.File)
	}
	var lc *lumberjack.Logger
	if l.Rotation {
		lc = NewLumberjackConfigDefault()
		lc.MaxSize = l.MaxSize
		lc.MaxAge = l.MaxAge
		lc.MaxBackups = l.MaxBackups
	}
	lg, err := NewZapLoggerWithConf(zc, lc, zap.AddCallerSkip(1))
	if err != nil {
		panic(err)
	}
	return lg
}

func Init(l *Config) {
	logger = NewLogger(l)
}

func Log() *zap.Logger {
	return logger
}

func ParseZapLevel(lvl string) (zap.AtomicLevel, error) {
	var level zapcore.Level
	if err := level.Set(lvl); err != nil {
		return zap.NewAtomicLevel(), err
	}
	return zap.NewAtomicLevelAt(level), nil
}
