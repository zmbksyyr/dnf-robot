package scheduler

import (
	"errors"
	"net"
	"strconv"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

func (m *RobotManager) StartAutoActions() {
	m.autoMu.Lock()
	if m.supervisor != nil {
		m.autoMu.Unlock()
		return
	}
	runtime := NewRobotRuntime(m)
	supervisor := NewRobotSupervisor(m, runtime)
	m.supervisor = supervisor
	m.autoStoreBusy = make(map[int]bool)
	m.autoPortSince = time.Time{}
	m.autoPortReady = false
	m.autoPortLog = time.Time{}
	m.autoEnabled = true
	m.autoMu.Unlock()
	supervisor.Start()
}

func (m *RobotManager) StopAutoActions() {
	if err := m.stopAutoActions(); err != nil {
		robotLogf("[RobotManager] stop_auto_incomplete err=%v\n", err)
	}
}

func (m *RobotManager) stopAutoActions() error {
	m.autoMu.Lock()
	supervisor := m.supervisor
	m.autoEnabled = false
	m.autoMu.Unlock()
	if supervisor == nil {
		return nil
	}
	if err := supervisor.StopWithError(); err != nil {
		return err
	}
	m.autoMu.Lock()
	if m.supervisor == supervisor {
		m.supervisor = nil
	}
	m.autoMu.Unlock()
	return nil
}

func (m *RobotManager) Shutdown() error {
	autoErr := m.stopAutoActions()
	if m.positionWrites == nil {
		return autoErr
	}
	return errors.Join(autoErr, m.positionWrites.Close())
}

func (m *RobotManager) SetAutoEnabled(enabled bool) robotcap.AutoStatus {
	_ = m.writeRobotConfigValues(map[string]string{
		"auto.auto_actions": strconv.FormatBool(enabled),
	})
	m.autoMu.Lock()
	supervisor := m.supervisor
	m.autoEnabled = enabled
	m.autoMu.Unlock()
	if !enabled && supervisor != nil {
		end := m.beginActorContainerOp("auto_stop")
		defer end()
		rc := m.loadRobotConfig()
		supervisor.stopAutoActors(true)
		summary := robotcap.SummarizeRuntimeStatusMap(m.runtimeStatusMap())
		running, connecting, stores := summary.Running, summary.Connecting, summary.Stores
		m.updateAutoSnapshot(rc, running, connecting, stores)
		m.updateAutoActorSnapshot(supervisor.actorCounts(time.Now(), rc))
		m.updateSchedulerStatus(rc, m.adaptiveSchedulerSignals(), schedulerPolicyDecision{Mode: schedulerPolicyManual, Reason: schedulerReasonAutoDisabled})
	}
	return m.AutoStatus()
}

func (m *RobotManager) AutoStatus() robotcap.AutoStatus {
	rc := m.loadRobotConfig()
	summary := m.runtimeStatusSummarySnapshot()
	running, connecting, stores := summary.Running, summary.Connecting, summary.Stores
	m.autoMu.Lock()
	out := m.autoStats
	out.Enabled = m.autoEnabled && rc.AutoActions
	m.autoMu.Unlock()
	out.TargetOnline = rc.AutoTargetOnlineCount
	out.Running = running
	out.Connecting = connecting
	if out.GamePortAddress == "" {
		out.GamePortAddress = m.robotGamePortAddress()
	}
	out.StoreProbability = rc.AutoStoreProbabilityPercent
	out.StoreRunning = stores
	out.UpdatedAt = time.Now()
	return out
}

func (m *RobotManager) autoActionsEnabled(rc robotconfig.RuntimeConfig) bool {
	m.autoMu.Lock()
	enabled := m.autoEnabled
	m.autoMu.Unlock()
	return enabled && rc.AutoActions
}

func (m *RobotManager) autoGamePortStable(now time.Time, rc robotconfig.RuntimeConfig) bool {
	addr := m.robotGamePortAddress()
	timeout := time.Duration(rc.AutoGamePortCheckTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	open := err == nil
	if conn != nil {
		_ = conn.Close()
	}

	stableFor := time.Duration(rc.AutoGamePortStableSec) * time.Second
	if stableFor <= 0 {
		stableFor = 15 * time.Second
	}

	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	m.autoStats.GamePortAddress = addr
	if !open {
		if m.autoPortReady || now.Sub(m.autoPortLog) >= 10*time.Second {
			robotLogf("[AutoGate] game_port_not_ready addr=%s err=%v\n", addr, err)
			m.autoPortLog = now
		}
		m.autoPortSince = time.Time{}
		m.autoPortReady = false
		m.autoStats.GamePortReady = false
		m.autoStats.GamePortStableAt = time.Time{}
		m.autoStats.UpdatedAt = now
		return false
	}
	if m.autoPortSince.IsZero() {
		m.autoPortSince = now
	}
	stableAt := m.autoPortSince.Add(stableFor)
	m.autoStats.GamePortStableAt = stableAt
	if now.Before(stableAt) {
		if now.Sub(m.autoPortLog) >= 10*time.Second {
			robotLogf("[AutoGate] game_port_wait_stable addr=%s stable_at=%s\n", addr, stableAt.Format(time.RFC3339))
			m.autoPortLog = now
		}
		m.autoPortReady = false
		m.autoStats.GamePortReady = false
		m.autoStats.UpdatedAt = now
		return false
	}
	if !m.autoPortReady {
		robotLogf("[AutoGate] game_port_stable addr=%s stable_for=%s\n", addr, stableFor)
	}
	m.autoPortReady = true
	m.autoStats.GamePortReady = true
	m.autoStats.UpdatedAt = now
	return true
}
