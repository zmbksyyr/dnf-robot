package scheduler

import (
	"math/rand"
	"net"
	"robot/internal/capability/catalog"
	"robot/internal/shared"
	"strconv"
	"strings"
	"time"
)

func (m *RobotManager) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	var out int
	_ = m.withRand(func(r *rand.Rand) {
		out = r.Intn(n)
	})
	return out
}

func (m *RobotManager) randBetween(min, max int) int {
	if max < min {
		min, max = max, min
	}
	return min + m.randIntn(max-min+1)
}

func (m *RobotManager) randomFrom(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	return vals[m.randIntn(len(vals))]
}

func (m *RobotManager) randomString(vals []string, fallback string) string {
	if len(vals) == 0 {
		return fallback
	}
	return vals[m.randIntn(len(vals))]
}

func (m *RobotManager) randShuffle(n int, swap func(i, j int)) {
	if n <= 1 {
		return
	}
	_ = m.withRand(func(r *rand.Rand) {
		r.Shuffle(n, swap)
	})
}

func (m *RobotManager) withRand(fn func(*rand.Rand)) error {
	return m.lockHub().WithResource("scheduler", "random", "random_source", func() error {
		fn(m.rand)
		return nil
	})
}

func (m *RobotManager) beginStoreBusy(uid int) bool {
	if uid <= 0 {
		return false
	}
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.autoStoreBusy == nil {
		m.autoStoreBusy = make(map[int]bool)
	}
	if m.autoStoreBusy[uid] {
		return false
	}
	m.autoStoreBusy[uid] = true
	return true
}

func (m *RobotManager) endStoreBusy(uid int) {
	if uid <= 0 {
		return
	}
	m.autoMu.Lock()
	delete(m.autoStoreBusy, uid)
	m.autoMu.Unlock()
}

func (m *RobotManager) markCleanupPending(uids []int) {
	if len(uids) == 0 {
		return
	}
	now := time.Now()
	_ = m.lockHub().WithResource("scheduler", "cleanup-pending", "mark_cleanup_pending", func() error {
		if m.cleanupPendingUIDs == nil {
			m.cleanupPendingUIDs = make(map[int]time.Time)
		}
		for _, uid := range uids {
			if uid > 0 {
				m.cleanupPendingUIDs[uid] = now
			}
		}
		return nil
	})
}

func (m *RobotManager) clearCleanupPending(uids []int) {
	if len(uids) == 0 {
		return
	}
	_ = m.lockHub().WithResource("scheduler", "cleanup-pending", "clear_cleanup_pending", func() error {
		for _, uid := range uids {
			delete(m.cleanupPendingUIDs, uid)
		}
		return nil
	})
}

func (m *RobotManager) cleanupPendingSet() map[int]bool {
	out := map[int]bool{}
	_ = m.lockHub().WithResource("scheduler", "cleanup-pending", "cleanup_pending_set", func() error {
		out = make(map[int]bool, len(m.cleanupPendingUIDs))
		for uid := range m.cleanupPendingUIDs {
			out[uid] = true
		}
		return nil
	})
	return out
}

func (m *RobotManager) loadMapCatalog() []shared.MapCatalogItem {
	if m.cfg == nil {
		return nil
	}
	return catalog.Maps(m.cfg.ConfigDir)
}

func (m *RobotManager) robotConnectIP() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.RobotConnectIP) != "" {
		return strings.TrimSpace(m.cfg.RobotConnectIP)
	}
	if m.cfg != nil {
		return strings.TrimSpace(m.cfg.RobotInnerIP)
	}
	return ""
}

func (m *RobotManager) robotGamePortAddress() string {
	port := 10011
	if m.cfg != nil && m.cfg.RobotGamePort > 0 {
		port = m.cfg.RobotGamePort
	}
	host := strings.TrimSpace(m.robotConnectIP())
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
