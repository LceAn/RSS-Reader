// in utils/logger.go

package utils

import (
	"fmt"
	"os"
	"time"
)

// ANSI Color Codes
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
)

// Logger struct to hold component name
type Logger struct {
	Component string
}

// NewLogger creates a new logger with a specific component tag
func NewLogger(component string) *Logger {
	return &Logger{Component: component}
}

// formatLog creates the standard log string
func (l *Logger) formatLog(level, levelColor, message string, fields ...interface{}) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedMessage := fmt.Sprintf(message, fields...)
	return fmt.Sprintf("%s [%s%s%s] [%s] - %s", timestamp, levelColor, level, ColorReset, l.Component, formattedMessage)
}

// Info logs a message with level INFO
func (l *Logger) Info(message string, fields ...interface{}) {
	logString := l.formatLog("INFO", ColorGreen, message, fields...)
	fmt.Fprintln(os.Stderr, logString) // 使用 fmt 直接输出，绕过 log 包的前缀
}

// Warn logs a message with level WARN
func (l *Logger) Warn(message string, fields ...interface{}) {
	logString := l.formatLog("WARN", ColorYellow, message, fields...)
	fmt.Fprintln(os.Stderr, logString) // 使用 fmt 直接输出
}

// Error logs a message with level ERROR
func (l *Logger) Error(message string, fields ...interface{}) {
	logString := l.formatLog("ERROR", ColorRed, message, fields...)
	fmt.Fprintln(os.Stderr, logString) // 使用 fmt 直接输出
}

// Fatal logs a message with level FATAL and exits
func (l *Logger) Fatal(message string, fields ...interface{}) {
	logString := l.formatLog("FATAL", ColorRed, message, fields...)
	fmt.Fprintln(os.Stderr, logString) // 使用 fmt 直接输出
	os.Exit(1)
}

// System logs a message with level INFO and component SYSTEM (for general use)
func System(message string, fields ...interface{}) {
	logger := NewLogger("SYSTEM")
	logString := logger.formatLog("INFO", ColorBlue, message, fields...)
	fmt.Fprintln(os.Stderr, logString) // 使用 fmt 直接输出
}