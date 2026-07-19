package dnf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogCloseFlushesBufferedRecords(t *testing.T) {
	path := prepareTestLog(t, time.Hour, defaultMaxLogSize, 2)
	LogString(LogLevelIndispensable, "buffered-close-record\n")
	LogClose()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "buffered-close-record") || !strings.Contains(text, "LOG END") {
		t.Fatalf("closed log missing buffered records: %q", text)
	}
}

func TestLogFatalFlushesImmediately(t *testing.T) {
	path := prepareTestLog(t, time.Hour, defaultMaxLogSize, 2)
	LogSetLevel(LogLevelInfo)
	LogString(LogLevelFatal, "fatal-record\n")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fatal-record") {
		t.Fatalf("fatal record was not flushed: %q", data)
	}
}

func TestLogPeriodicFlush(t *testing.T) {
	path := prepareTestLog(t, 10*time.Millisecond, defaultMaxLogSize, 2)
	LogString(LogLevelIndispensable, "periodic-record\n")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "periodic-record") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("periodic flush did not write buffered record")
}

func TestLogRotationKeepsConfiguredFileBounds(t *testing.T) {
	const maxBytes = int64(256)
	path := prepareTestLog(t, time.Hour, maxBytes, 2)
	for i := 0; i < 20; i++ {
		LogString(LogLevelIndispensable, "bounded-record-abcdefghijklmnopqrstuvwxyz\n")
	}
	LogClose()

	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxBytes {
			t.Fatalf("%s size=%d exceeds %d", candidate, info.Size(), maxBytes)
		}
	}
}

func TestLogConcurrentWritersRotationAndClose(t *testing.T) {
	const maxBytes = int64(512)
	path := prepareTestLog(t, time.Millisecond, maxBytes, 3)
	const writers = 16
	const records = 100
	start := make(chan struct{})
	var writersWG sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		writersWG.Add(1)
		go func(writer int) {
			defer writersWG.Done()
			<-start
			for record := 0; record < records; record++ {
				LogString(LogLevelIndispensable, fmt.Sprintf("writer=%d record=%d\n", writer, record))
			}
		}(writer)
	}
	close(start)
	writersWG.Wait()

	closeDone := make(chan struct{})
	var closeWG sync.WaitGroup
	for i := 0; i < 2; i++ {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			LogClose()
		}()
	}
	go func() {
		closeWG.Wait()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent LogClose deadlocked")
	}

	for _, candidate := range []string{path, path + ".1", path + ".2", path + ".3"} {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > maxBytes {
			t.Fatalf("%s size=%d exceeds %d", candidate, info.Size(), maxBytes)
		}
	}

	reopened := filepath.Join(t.TempDir(), "reopened.log")
	if err := LogInit(reopened); err != nil {
		t.Fatalf("reinitialize after concurrent close: %v", err)
	}
}

func BenchmarkLogStringBuffered(b *testing.B) {
	path := filepath.Join(b.TempDir(), "log_robot")
	resetLogGlobals()
	logMu.Lock()
	logFlushInterval = time.Hour
	logMaxSize = 1 << 30
	logBackups = 1
	logMu.Unlock()
	if err := LogInit(path); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		LogClose()
		resetLogGlobals()
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LogString(LogLevelIndispensable, "benchmark-record\n")
	}
}

func BenchmarkLogWriteAndSyncBaseline(b *testing.B) {
	file, err := os.OpenFile(filepath.Join(b.TempDir(), "sync.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = file.Close() })
	record := []byte("benchmark-record\n")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := file.Write(record); err != nil {
			b.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			b.Fatal(err)
		}
	}
}

func prepareTestLog(t *testing.T, interval time.Duration, maxBytes int64, backups int) string {
	t.Helper()
	LogClose()
	resetLogGlobals()
	logMu.Lock()
	logFlushInterval = interval
	logMaxSize = maxBytes
	logBackups = backups
	logMu.Unlock()
	path := filepath.Join(t.TempDir(), "log_robot")
	if err := LogInit(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		LogClose()
		resetLogGlobals()
	})
	return path
}

func resetLogGlobals() {
	logMu.Lock()
	logLevel = LogLevelIndispensable
	logMaxSize = defaultMaxLogSize
	logBackups = defaultMaxLogBackups
	logFlushInterval = time.Second
	logMu.Unlock()
}
