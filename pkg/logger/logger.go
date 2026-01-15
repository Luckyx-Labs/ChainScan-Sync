package logger

import (
	"os"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

// InitLogger initialize the logger
func InitLogger(cfg config.LogConfig) error {
	log = logrus.New()

	// set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	// set log format
	if cfg.Format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	}

	// set output
	if cfg.Output == "file" && cfg.FilePath != "" {
		file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return err
		}
		log.SetOutput(file)
	} else {
		log.SetOutput(os.Stdout)
	}

	return nil
}

// GetLogger get the logger instance
func GetLogger() *logrus.Logger {
	if log == nil {
		log = logrus.New()
	}
	return log
}

// WithField create a log entry with a field
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// WithFields create a log entry with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}

// Debug Debug log
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// Info Info log
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Warn Warn log
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Error Error log
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Fatal Fatal log
func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

// Debugf Debug formatted log
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Infof Info formatted log
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Warnf Warn formatted log
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Errorf Error formatted log
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatalf Fatal formatted log
func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}
