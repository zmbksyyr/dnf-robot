package store

import "robot/internal/foundation/lockhub"

type worldHornCacheEntry struct {
	done chan struct{}
	err  error
}

// WorldHornCache keeps successful inventory verification for the lifetime of
// the manager. Failed checks are removed so a later login can retry them.
type WorldHornCache struct {
	access  lockhub.Locker
	entries map[int]*worldHornCacheEntry
}

func NewWorldHornCache() *WorldHornCache {
	return &WorldHornCache{entries: make(map[int]*worldHornCacheEntry)}
}

func (c *WorldHornCache) Ensure(cid int, verify func() error) error {
	if verify == nil {
		return nil
	}
	if c == nil || cid <= 0 {
		return verify()
	}
	c.access.Lock()
	if entry := c.entries[cid]; entry != nil {
		c.access.Unlock()
		<-entry.done
		return entry.err
	}
	if c.entries == nil {
		c.entries = make(map[int]*worldHornCacheEntry)
	}
	entry := &worldHornCacheEntry{done: make(chan struct{})}
	c.entries[cid] = entry
	c.access.Unlock()

	err := verify()
	c.access.Lock()
	entry.err = err
	if err != nil && c.entries[cid] == entry {
		delete(c.entries, cid)
	}
	close(entry.done)
	c.access.Unlock()
	return err
}

func (c *WorldHornCache) Invalidate(cid int) {
	if c == nil || cid <= 0 {
		return
	}
	c.access.Lock()
	delete(c.entries, cid)
	c.access.Unlock()
}
