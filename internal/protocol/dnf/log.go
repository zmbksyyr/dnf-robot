package dnf

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"robot/internal/foundation/lockhub"
	"robot/internal/foundation/logfile"
)

// ---- log.go ----
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
	defaultLogBufferSize       = 64 * 1024
)

var (
	logFile          *os.File
	logWriter        *bufio.Writer
	logMu            lockhub.Locker
	logLevel         LogLevel = LogLevelIndispensable
	logName          string
	logSize          int64
	logMaxSize       int64 = defaultMaxLogSize
	logBackups             = defaultMaxLogBackups
	logFlushInterval       = time.Second
	logFlushStop     chan struct{}
	logFlushDone     chan struct{}
	logClosing       bool
)

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
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil || logFlushStop != nil || logClosing {
		return fmt.Errorf("log already initialized: %s", logName)
	}
	if err := logfile.Prepare(path, logMaxSize, logBackups); err != nil {
		return err
	}
	var err error
	logFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := logFile.Stat()
	if err != nil {
		_ = logFile.Close()
		logFile = nil
		return err
	}
	logName = path
	logSize = info.Size()
	logWriter = bufio.NewWriterSize(logFile, defaultLogBufferSize)
	startLogFlusherLocked()
	logStringLocked(LogLevelIndispensable, "LOG START\n")
	flushLogLocked(false)
	return nil
}

func LogSetLevel(level LogLevel) {
	logMu.Lock()
	logLevel = level
	logMu.Unlock()
}

func LogString(level LogLevel, msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	logStringLocked(level, msg)
}

func logStringLocked(level LogLevel, msg string) {
	if logFile == nil || logWriter == nil {
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
		rotateLogIfNeededLocked(len(prefix))
		if logFile == nil || logWriter == nil {
			return
		}
		n, err := logWriter.WriteString(prefix)
		logSize += int64(n)
		if err != nil {
			fmt.Printf("[Log] write failed path=%s err=%v\n", logName, err)
			return
		}
		if level == LogLevelFatal {
			flushLogLocked(true)
		}
	}
}

func rotateLogIfNeededLocked(nextBytes int) {
	if logFile == nil || logWriter == nil || logName == "" || nextBytes <= 0 {
		return
	}
	if logSize == 0 || logSize+int64(nextBytes) <= logMaxSize {
		return
	}
	flushLogLocked(false)
	_ = logFile.Close()
	logFile = nil
	logWriter = nil
	if err := logfile.Rotate(logName, logBackups); err != nil {
		fmt.Printf("[Log] rotate failed path=%s err=%v\n", logName, err)
	}
	var openErr error
	logFile, openErr = os.OpenFile(logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if openErr != nil {
		fmt.Printf("[Log] reopen failed path=%s err=%v\n", logName, openErr)
		logFile = nil
		logSize = 0
		return
	}
	info, err := logFile.Stat()
	if err != nil {
		fmt.Printf("[Log] stat failed path=%s err=%v\n", logName, err)
		_ = logFile.Close()
		logFile = nil
		logSize = 0
		return
	}
	logSize = info.Size()
	logWriter = bufio.NewWriterSize(logFile, defaultLogBufferSize)
}

func LogClose() {
	logMu.Lock()
	if logClosing {
		done := logFlushDone
		logMu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	if logFile == nil && logFlushStop == nil {
		logMu.Unlock()
		return
	}
	logClosing = true
	if logFile != nil {
		logStringLocked(LogLevelIndispensable, "LOG END\n")
		flushLogLocked(true)
		_ = logFile.Close()
	}
	logFile = nil
	logWriter = nil
	logSize = 0
	stop := logFlushStop
	done := logFlushDone
	if stop != nil {
		close(stop)
	}
	logMu.Unlock()
	if done != nil {
		<-done
	}
	logMu.Lock()
	if logFlushStop == stop {
		logFlushStop = nil
		logFlushDone = nil
	}
	logClosing = false
	logMu.Unlock()
}

func flushLogLocked(sync bool) {
	if logFile == nil || logWriter == nil {
		return
	}
	if err := logWriter.Flush(); err != nil {
		fmt.Printf("[Log] flush failed path=%s err=%v\n", logName, err)
		return
	}
	if sync {
		if err := logFile.Sync(); err != nil {
			fmt.Printf("[Log] sync failed path=%s err=%v\n", logName, err)
		}
	}
}

func startLogFlusherLocked() {
	interval := logFlushInterval
	if interval <= 0 {
		interval = time.Second
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	logFlushStop = stop
	logFlushDone = done
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logMu.Lock()
				flushLogLocked(false)
				logMu.Unlock()
			case <-stop:
				return
			}
		}
	}()
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
