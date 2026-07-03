package lockhub

import (
	"fmt"
	"strconv"
	"sync"
)

// ---- lockhub.go ----
type Key struct {
	Scope string
	ID    string
}

func RobotKey(uid int) Key {
	return Key{Scope: "robot", ID: fmt.Sprint(uid)}
}

func ResourceKey(resource, id string) Key {
	return Key{Scope: resource, ID: id}
}

func (k Key) String() string {
	if k.Scope == "" {
		return k.ID
	}
	return k.Scope + ":" + k.ID
}

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

func (h *Hub) WithKey(key Key, reason string, fn func() error) error {
	if key.Scope == "robot" {
		uid, err := strconv.Atoi(key.ID)
		if err == nil {
			return h.WithRobot(uid, reason, fn)
		}
	}
	return h.WithResource(key.Scope, key.ID, reason, fn)
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

// ---- refhub.go ----
type RefHub struct {
	mu    sync.Mutex
	locks map[int]*RefLock
}

type RefLock struct {
	mu   sync.Mutex
	refs int
}

func NewRefHub() *RefHub {
	return &RefHub{locks: make(map[int]*RefLock)}
}

func (h *RefHub) Acquire(uid int) *RefLock {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.locks == nil {
		h.locks = make(map[int]*RefLock)
	}
	lock := h.locks[uid]
	if lock == nil {
		lock = &RefLock{}
		h.locks[uid] = lock
	}
	lock.refs++
	lock.mu.Lock()
	return lock
}

func (h *RefHub) Release(uid int, lock *RefLock) {
	if lock == nil {
		return
	}
	lock.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	lock.refs--
	if lock.refs <= 0 && h.locks[uid] == lock {
		delete(h.locks, uid)
	}
}

func (h *RefHub) Active(uid int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.locks[uid] != nil
}
