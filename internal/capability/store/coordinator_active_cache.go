package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"robot/internal/foundation/atomicfile"
	"robot/internal/foundation/lockhub"
)

const (
	ActivePointCacheFile = "store_points_active.json"
	activePointCacheVer  = 1
	activePointSaveDelay = 250 * time.Millisecond
)

type activePointCacheWriter func(path string, data []byte) error

type activePointPersistence struct {
	access     lockhub.Locker
	timer      *time.Timer
	done       chan struct{}
	generation uint64
	pending    bool
	flushing   bool
	delay      time.Duration
	write      activePointCacheWriter
}

type ActivePointCache struct {
	Version     int                    `json:"version"`
	SourceMD5   string                 `json:"source_md5"`
	Updated     string                 `json:"updated_at"`
	Occupancies []ActivePointOccupancy `json:"occupancies"`
}

type ActivePointOccupancy struct {
	PointID string `json:"point_id"`
	Until   string `json:"until"`
}

// scheduleActiveOccupancySave coalesces bursts of store starts and releases.
// Only one timer or writer is active at a time, while Flush cancels and joins
// it before publishing the final synchronous snapshot.
func (c *PointCoordinator) scheduleActiveOccupancySave() {
	if c == nil || c.configDir == "" {
		return
	}
	c.activeSave.access.Lock()
	defer c.activeSave.access.Unlock()
	if c.activeSave.flushing || c.activeSave.timer != nil {
		c.activeSave.pending = true
		return
	}
	c.scheduleActiveOccupancySaveLocked()
}

func (c *PointCoordinator) scheduleActiveOccupancySaveLocked() {
	c.activeSave.generation++
	generation := c.activeSave.generation
	done := make(chan struct{})
	c.activeSave.done = done
	c.activeSave.pending = false
	delay := c.activeSave.delay
	if delay <= 0 {
		delay = activePointSaveDelay
	}
	c.activeSave.timer = time.AfterFunc(delay, func() {
		c.runScheduledActiveOccupancySave(generation, done)
	})
}

func (c *PointCoordinator) runScheduledActiveOccupancySave(generation uint64, done chan struct{}) {
	defer func() {
		if rec := recover(); rec != nil {
			c.logActiveOccupancySavePanic(rec)
		}
		c.finishScheduledActiveOccupancySave(generation, done)
	}()
	c.saveActiveOccupancies()
}

func (c *PointCoordinator) finishScheduledActiveOccupancySave(generation uint64, done chan struct{}) {
	c.activeSave.access.Lock()
	if c.activeSave.generation != generation || c.activeSave.done != done {
		c.activeSave.access.Unlock()
		close(done)
		return
	}
	dirty := c.activeOccupanciesDirty()
	retry := c.activeSave.pending && dirty && !c.activeSave.flushing
	c.activeSave.timer = nil
	c.activeSave.done = nil
	c.activeSave.pending = false
	close(done)
	if retry {
		c.scheduleActiveOccupancySaveLocked()
	}
	c.activeSave.access.Unlock()
}

func (c *PointCoordinator) beginActiveOccupancyFlush() {
	c.activeSave.access.Lock()
	c.activeSave.flushing = true
	c.activeSave.pending = false
	c.activeSave.generation++
	timer := c.activeSave.timer
	done := c.activeSave.done
	c.activeSave.timer = nil
	c.activeSave.done = nil
	stopped := timer != nil && timer.Stop()
	c.activeSave.access.Unlock()

	if done == nil {
		return
	}
	if stopped {
		close(done)
		return
	}
	<-done
}

func (c *PointCoordinator) endActiveOccupancyFlush() {
	c.activeSave.access.Lock()
	dirty := c.activeOccupanciesDirty()
	queuedDuringFlush := c.activeSave.pending
	c.activeSave.pending = false
	c.activeSave.flushing = false
	if queuedDuringFlush && dirty {
		c.scheduleActiveOccupancySaveLocked()
	}
	c.activeSave.access.Unlock()
}

func (c *PointCoordinator) activeOccupanciesDirty() bool {
	c.pointMu.Lock()
	dirty := c.activeDirty > 0
	c.pointMu.Unlock()
	return dirty
}

func (c *PointCoordinator) logActiveOccupancySavePanic(rec interface{}) {
	defer func() { _ = recover() }()
	c.logf("[StorePoint] active_cache_panic err=%v\n", rec)
}

func (c *PointCoordinator) saveActiveOccupancies() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.pointMu.Lock()
	if c.configDir == "" || c.activeDirty == 0 {
		c.pointMu.Unlock()
		return
	}
	dirty := c.activeDirty
	configDir := c.configDir
	now := time.Now()
	cache := ActivePointCache{
		Version:     activePointCacheVer,
		SourceMD5:   c.sourceMD5,
		Updated:     now.Format(time.RFC3339),
		Occupancies: c.activeOccupanciesLocked(now),
	}
	c.pointMu.Unlock()

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		c.logf("[StorePoint] active_cache_encode_failed err=%v\n", err)
		return
	}
	cachePath := filepath.Join(configDir, ActivePointCacheFile)
	write := c.activeSave.write
	if write == nil {
		write = func(path string, data []byte) error {
			return atomicfile.WriteFile(path, data, 0644)
		}
	}
	if err := write(cachePath, data); err != nil {
		c.logf("[StorePoint] active_cache_write_failed err=%v\n", err)
		return
	}
	c.pointMu.Lock()
	c.activeDirty -= dirty
	if c.activeDirty < 0 {
		c.activeDirty = 0
	}
	c.pointMu.Unlock()
}

func (c *PointCoordinator) loadActiveOccupancies() int {
	data, err := os.ReadFile(filepath.Join(c.configDir, ActivePointCacheFile))
	if err != nil {
		return 0
	}
	var cache ActivePointCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != activePointCacheVer || cache.SourceMD5 != c.sourceMD5 {
		return 0
	}
	return c.restoreActiveOccupanciesLocked(cache.Occupancies, time.Now())
}
