package lockhub

import (
	"sync"
)

type Hub struct {
	global    sync.Mutex
	mu        sync.Mutex
	robots    map[int]*sync.Mutex
	resources map[string]*sync.Mutex
}

type Locker struct {
	mu sync.Mutex
}

type RWLocker struct {
	mu sync.RWMutex
}

func (l *Locker) Lock() {
	l.mu.Lock()
}

func (l *Locker) TryLock() bool {
	return l.mu.TryLock()
}

func (l *Locker) Unlock() {
	l.mu.Unlock()
}

func (l *RWLocker) Lock() {
	l.mu.Lock()
}

func (l *RWLocker) Unlock() {
	l.mu.Unlock()
}

func (l *RWLocker) RLock() {
	l.mu.RLock()
}

func (l *RWLocker) RUnlock() {
	l.mu.RUnlock()
}

func New() *Hub {
	return &Hub{
		robots:    make(map[int]*sync.Mutex),
		resources: make(map[string]*sync.Mutex),
	}
}

func (h *Hub) WithGlobal(_ string, fn func() error) error {
	h.global.Lock()
	defer h.global.Unlock()
	return fn()
}

func (h *Hub) WithRobot(uid int, _ string, fn func() error) error {
	l := h.robotLock(uid)
	l.Lock()
	defer l.Unlock()
	return fn()
}

func (h *Hub) WithResource(resource, key, _ string, fn func() error) error {
	l := h.resourceLock(resource + ":" + key)
	l.Lock()
	defer l.Unlock()
	return fn()
}

func (h *Hub) robotLock(uid int) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l := h.robots[uid]; l != nil {
		return l
	}
	l := &sync.Mutex{}
	h.robots[uid] = l
	return l
}

func (h *Hub) resourceLock(name string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l := h.resources[name]; l != nil {
		return l
	}
	l := &sync.Mutex{}
	h.resources[name] = l
	return l
}

func (h *Hub) ActiveLocks() (robots int, resources int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.robots), len(h.resources)
}
