package dnf

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type LogLevel int

const (
	LogLevelInfo          LogLevel = 0
	LogLevelWarning       LogLevel = 1
	LogLevelError         LogLevel = 2
	LogLevelFatal         LogLevel = 3
	LogLevelIndispensable LogLevel = 4
)

const (
	defaultMaxLogSize    int64 = 100 * 1024 * 1024
	defaultMaxLogBackups       = 5
)

var (
	logFile    *os.File
	logMu      sync.Mutex
	logLevel   LogLevel = LogLevelIndispensable
	logName    string
	logMaxSize int64 = defaultMaxLogSize
	logBackups       = defaultMaxLogBackups
)

func SetLogFileName(name string) {
	logName = name
}

func ConfigureLogRotation(maxSizeMB, backups int) {
	logMu.Lock()
	defer logMu.Unlock()
	if maxSizeMB <= 0 {
		maxSizeMB = 100
	}
	if backups <= 0 {
		backups = 5
	}
	logMaxSize = int64(maxSizeMB) * 1024 * 1024
	logBackups = backups
}

func LogInit(path string) error {
	var err error
	logFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	SetLogFileName(path)
	LogString(LogLevelIndispensable, "LOG START\n")
	return nil
}

func LogSetLevel(level LogLevel) {
	logLevel = level
}

func LogString(level LogLevel, msg string) {
	logMu.Lock()
	defer logMu.Unlock()

	if logFile == nil {
		return
	}

	now := time.Now()
	ts := fmt.Sprintf("%02d/%02d/%02d %02d:%02d:%02d",
		now.Day(), now.Month(), now.Year()%100,
		now.Hour(), now.Minute(), now.Second())

	var needLog bool
	var prefix string

	switch level {
	case LogLevelInfo:
		prefix = fmt.Sprintf("[INFO: %s] %s", ts, msg)
		needLog = false
	case LogLevelWarning:
		if int(LogLevelWarning) >= int(logLevel) {
			needLog = true
			prefix = fmt.Sprintf("[WARNING: %s] %s", ts, msg)
		}
	case LogLevelError:
		if int(LogLevelError) >= int(logLevel) {
			needLog = true
			prefix = fmt.Sprintf("[ERROR: %s] %s", ts, msg)
		}
	case LogLevelFatal:
		if int(LogLevelFatal) >= int(logLevel) {
			needLog = true
			prefix = fmt.Sprintf("[FATAL: %s] %s", ts, msg)
		}
	case LogLevelIndispensable:
		needLog = true
		prefix = fmt.Sprintf("[%s] %s", ts, msg)
	}

	if needLog {
		rotateLogIfNeededLocked()
		if logFile == nil {
			return
		}
		if _, err := logFile.WriteString(prefix); err != nil {
			fmt.Printf("[Log] write failed path=%s err=%v\n", logName, err)
			return
		}
		_ = logFile.Sync()
	}
}

func rotateLogIfNeededLocked() {
	if logFile == nil || logName == "" {
		return
	}
	info, err := logFile.Stat()
	if err != nil || info.Size() < logMaxSize {
		return
	}
	_ = logFile.Close()
	_ = os.Remove(fmt.Sprintf("%s.%d", logName, logBackups))
	for i := logBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", logName, i)
		dst := fmt.Sprintf("%s.%d", logName, i+1)
		if _, err := os.Stat(src); err == nil {
			_ = os.Remove(dst)
			_ = os.Rename(src, dst)
		}
	}
	_ = os.Remove(fmt.Sprintf("%s.1", logName))
	if err := os.Rename(logName, fmt.Sprintf("%s.1", logName)); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[Log] rotate failed path=%s err=%v\n", logName, err)
	}
	var openErr error
	logFile, openErr = os.OpenFile(logName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if openErr != nil {
		fmt.Printf("[Log] reopen failed path=%s err=%v\n", logName, openErr)
		logFile = nil
	}
}

func LogClose() {
	if logFile != nil {
		LogString(LogLevelIndispensable, "LOG END\n")
		logFile.Close()
		logFile = nil
	}
}

func PrintfGreen(format string, args ...interface{}) {
	fmt.Printf("\033[1;32m"+format+"\033[0m", args...)
}

func PrintfRed(format string, args ...interface{}) {
	fmt.Printf("\033[1;31m"+format+"\033[0m", args...)
}

func PrintfBlue(format string, args ...interface{}) {
	fmt.Printf("\033[1;36m"+format+"\033[0m", args...)
}
