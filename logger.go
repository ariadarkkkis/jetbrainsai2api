package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
	Fatal(format string, args ...any)
}

type AppLogger struct {
	logger *log.Logger
	debug  bool
}

func NewAppLogger() *AppLogger {
	return &AppLogger{
		logger: log.New(os.Stdout, "", log.LstdFlags),
		debug:  gin.Mode() == gin.DebugMode,
	}
}

func (l *AppLogger) Debug(format string, args ...any) {
	if l.debug {
		l.logger.Printf("[DEBUG] "+format, args...)
	}
}

func (l *AppLogger) Info(format string, args ...any) {
	l.logger.Printf("[INFO] "+format, args...)
}

func (l *AppLogger) Warn(format string, args ...any) {
	l.logger.Printf("[WARN] "+format, args...)
}

func (l *AppLogger) Error(format string, args ...any) {
	l.logger.Printf("[ERROR] "+format, args...)
}

func (l *AppLogger) Fatal(format string, args ...any) {
	l.logger.Fatalf("[FATAL] "+format, args...)
}

// 全局日志实例
var appLogger Logger = NewAppLogger()

// 全局日志函数
func Debug(format string, args ...any) { appLogger.Debug(format, args...) }
func Info(format string, args ...any)  { appLogger.Info(format, args...) }
func Warn(format string, args ...any)  { appLogger.Warn(format, args...) }
func Error(format string, args ...any) { appLogger.Error(format, args...) }
func Fatal(format string, args ...any) { appLogger.Fatal(format, args...) }
