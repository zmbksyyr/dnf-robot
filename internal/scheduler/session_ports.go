package scheduler

import (
	"fmt"
	"time"

	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	"robot/internal/shared"
)

const sessionWriteSafetyMargin = 2 * time.Second

func (m *RobotManager) waitAccountOffline(uid int, shouldStop func() bool) (bool, error) {
	if uid <= 0 {
		return false, fmt.Errorf("invalid account uid=%d", uid)
	}
	deadline := time.Now().Add(30 * time.Second)
	offlineStarted := time.Time{}
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return true, nil
		}
		online, err := m.schemaRepo().AccountOnline(uid)
		if err != nil {
			return false, err
		}
		if online {
			offlineStarted = time.Time{}
		} else if offlineStarted.IsZero() {
			offlineStarted = time.Now()
			// Logout performs this invalidation when the runtime disappears within
			// its first confirmation sample. Under load that close can take longer,
			// so complete the same boundary as soon as the account is observably
			// offline. A failed NoCache call keeps the existing timed safety window.
			if m.sessionReleaseRemaining(uid) > 0 {
				if err := m.invalidateClosedCharacterCache(uid); err != nil {
					robotLogf("[CharacterCache] uid=%d offline_nocache_failed fallback_ms=%d err=%v\n",
						uid, m.sessionReleaseRemaining(uid).Milliseconds(), err)
				}
			}
		} else if time.Since(offlineStarted) >= time.Second && m.sessionReleaseRemaining(uid) <= 0 {
			return false, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false, fmt.Errorf("account offline timeout uid=%d", uid)
}

func (m *RobotManager) sessionReleaseRemaining(uid int) time.Duration {
	if m == nil || uid <= 0 {
		return 0
	}
	delay := m.sessionReloginDelay
	if delay <= 0 {
		delay = 15 * time.Second
	}
	// Database writes must happen after the old game-session snapshot is fully
	// released, not exactly on the relogin boundary where a final save can race
	// and overwrite inventory or profession changes.
	delay += sessionWriteSafetyMargin
	m.sessionMu.Lock()
	lastLogout := m.sessionLastLogout[uid]
	m.sessionMu.Unlock()
	if lastLogout.IsZero() {
		return 0
	}
	remaining := delay - time.Since(lastLogout)
	if remaining > 0 {
		return remaining
	}
	return 0
}

func (m *RobotManager) sessionService() robotaction.SessionService {
	return robotaction.SessionService{Env: sessionActionEnv{manager: m}}
}

type sessionActionEnv struct {
	manager *RobotManager
}

func (e sessionActionEnv) CountRuntimeRunning() int {
	return e.manager.countRuntimeRunning()
}

func (e sessionActionEnv) EnsureWorldHornByCID(cid int) error {
	return e.manager.storePreparer().EnsureWorldHornByCID(cid)
}

func (e sessionActionEnv) InvalidateCharacterCache(uid int) error {
	return e.manager.invalidateClosedCharacterCache(uid)
}

func (e sessionActionEnv) RobotConnectIP() string {
	return e.manager.robotConnectIP()
}

func (e sessionActionEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
}

func (e sessionActionEnv) RuntimeStatusMapFresh() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMapFresh()
}

func (e sessionActionEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}

func (e sessionActionEnv) SendLogout(uid int) error {
	err := e.manager.doll.Logout(uid)
	if err == nil {
		e.manager.markSessionLogout(uid, time.Now())
	}
	return err
}

func (e sessionActionEnv) SendOnline(userinfos []shared.RuntimeOnlineUser) error {
	e.manager.waitSessionRelogin(userinfos)
	return e.manager.doll.Online(userinfos)
}

func (m *RobotManager) markSessionLogout(uid int, at time.Time) {
	if m == nil || uid <= 0 {
		return
	}
	m.sessionMu.Lock()
	if m.sessionLastLogout == nil {
		m.sessionLastLogout = make(map[int]time.Time)
	}
	m.sessionLastLogout[uid] = at
	m.sessionMu.Unlock()
}

func (m *RobotManager) clearSessionLogout(uid int) {
	if m == nil || uid <= 0 {
		return
	}
	m.sessionMu.Lock()
	delete(m.sessionLastLogout, uid)
	m.sessionMu.Unlock()
}

func (m *RobotManager) waitSessionRelogin(userinfos []shared.RuntimeOnlineUser) {
	if m == nil || len(userinfos) == 0 {
		return
	}
	delay := m.sessionReloginDelay
	if delay <= 0 {
		delay = 15 * time.Second
	}
	for {
		now := time.Now()
		wait := time.Duration(0)
		m.sessionMu.Lock()
		for _, userinfo := range userinfos {
			uid := userinfo.UID
			last := m.sessionLastLogout[uid]
			if uid <= 0 || last.IsZero() {
				continue
			}
			remaining := delay - now.Sub(last)
			if remaining <= 0 {
				delete(m.sessionLastLogout, uid)
				continue
			}
			if remaining > wait {
				wait = remaining
			}
		}
		m.sessionMu.Unlock()
		if wait <= 0 {
			return
		}
		time.Sleep(wait)
	}
}
