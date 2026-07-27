package lockhub

import "sync"

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
	if h.locks == nil {
		h.locks = make(map[int]*RefLock)
	}
	lock := h.locks[uid]
	if lock == nil {
		lock = &RefLock{}
		h.locks[uid] = lock
	}
	lock.refs++
	h.mu.Unlock()

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
