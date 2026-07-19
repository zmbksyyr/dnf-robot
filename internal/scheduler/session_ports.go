package scheduler

import (
	"time"

	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	"robot/internal/shared"
)

func (m *RobotManager) sessionService() robotaction.SessionService {
	return robotaction.SessionService{Env: sessionActionEnv{manager: m}}
}

type sessionActionEnv struct {
	manager *RobotManager
}

func (e sessionActionEnv) CountRuntimeRunning() int {
	return e.manager.countRuntimeRunning()
}

func (e sessionActionEnv) EnsureWorldHorn(uid int) error {
	return e.manager.storePreparer().EnsureWorldHorn(uid)
}

func (e sessionActionEnv) RobotConnectIP() string {
	return e.manager.robotConnectIP()
}

func (e sessionActionEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
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
