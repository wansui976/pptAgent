package log

import (
	"go.uber.org/zap"
)

var (
	logger *zap.Logger
	sugar  *zap.SugaredLogger
)

func Init() error {
	// Create logger
	logger, _ = zap.NewProductionConfig().Build()
	sugar = logger.Sugar()
	return nil
}

func init() {
	_ = Init()
}

func Info(msg string, fields ...interface{}) {
	sugar.Infof(msg, fields...)
}

func Infof(format string, args ...interface{}) {
	sugar.Infof(format, args...)
}

func Error(msg string, fields ...interface{}) {
	sugar.Errorf(msg, fields...)
}

func Errorf(format string, args ...interface{}) {
	sugar.Errorf(format, args...)
}

func Warn(msg string, fields ...interface{}) {
	sugar.Warnf(msg, fields...)
}

func Warnf(format string, args ...interface{}) {
	sugar.Warnf(format, args...)
}

func Sync() error {
	if logger != nil {
		return logger.Sync()
	}
	return nil
}
