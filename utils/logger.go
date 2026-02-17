package utils

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.SugaredLogger

// InitLogger initializes the global sugared logger with production-like settings.
func InitLogger() {
	cfg := zap.NewDevelopmentConfig()
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	cfg.EncoderConfig.TimeKey = "time"

	logger, err := cfg.Build()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	log = logger.Sugar()
}

// Logger returns the global sugared logger instance.
func Logger() *zap.SugaredLogger {
	if log == nil {
		InitLogger()
	}
	return log
}

// Sync flushes any buffered log entries — call this on program exit.
func Sync() {
	if log != nil {
		_ = log.Sync()
	}
}