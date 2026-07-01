package marketapp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const marketLogFile = "market_log.jsonl"

func marketLogPath(configDir string) string {
	return filepath.Join(configDir, marketLogFile)
}

func (a *App) appendLog(event LogEvent) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	path := marketLogPath(a.configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

func (a *App) logTail(limit int) []LogEvent {
	if limit <= 0 {
		limit = 20
	}
	f, err := os.Open(marketLogPath(a.configDir))
	if err != nil {
		return nil
	}
	defer f.Close()
	ring := make([]LogEvent, 0, limit)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var event LogEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if len(ring) < limit {
			ring = append(ring, event)
			continue
		}
		copy(ring, ring[1:])
		ring[len(ring)-1] = event
	}
	return ring
}
