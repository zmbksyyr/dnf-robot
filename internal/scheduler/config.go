package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"robot/internal/capability/catalog"
	"robot/internal/capability/keypair"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/foundation/config"
	"strings"
	"time"
)

func (m *RobotManager) invalidateRobotConfigCache() {
	m.withCache("invalidate_robot_config", func() {
		m.configSnapshot.Store(nil)
	})
}

func (m *RobotManager) ReleaseDefaultKeypair() (keypair.KeypairStatus, error) {
	return keypair.ReleaseDefault(m.cfg)
}

func (m *RobotManager) KeypairStatus() keypair.KeypairStatus {
	return keypair.CurrentStatus(m.cfg)
}

func (m *RobotManager) RobotConfig() (robotcap.ConfigResult, error) {
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	data, err := os.ReadFile(path)
	if err != nil {
		return robotcap.ConfigResult{}, err
	}
	return robotcap.ConfigResult{Path: path, Text: robotconfig.PublicText(string(data)), Config: robotconfig.Clone(m.loadRobotConfig())}, nil
}

func (m *RobotManager) UpdateRobotConfig(req robotcap.ConfigUpdateRequest) (robotcap.ConfigResult, error) {
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	if strings.TrimSpace(req.Text) != "" {
		if _, err := config.LoadFromString(req.Text); err != nil {
			return robotcap.ConfigResult{}, err
		}
		if err := os.WriteFile(path, []byte(req.Text), 0644); err != nil {
			return robotcap.ConfigResult{}, err
		}
		m.invalidateRobotConfigCache()
	} else if len(req.Updates) > 0 {
		values := make(map[string]string, len(req.Updates))
		for key, value := range req.Updates {
			values[key] = fmt.Sprint(value)
		}
		if err := m.writeRobotConfigValues(values); err != nil {
			return robotcap.ConfigResult{}, err
		}
	}
	return m.RobotConfig()
}

func (m *RobotManager) writeRobotConfigValues(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	path := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := robotconfig.UpdateINIText(string(data), values)
	if _, err := config.LoadFromString(text); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return err
	}
	m.invalidateRobotConfigCache()
	return nil
}

func fileModTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

const robotConfigCheckInterval = time.Second

type robotConfigSnapshot struct {
	base      robotconfig.RuntimeConfig
	effective robotconfig.RuntimeConfig
	modTime   time.Time
	checkedAt time.Time
}

// loadRobotConfig returns an immutable configuration view. Callers may modify
// scalar fields on the returned value, but must not mutate its slice fields.
func (m *RobotManager) loadRobotConfig() robotconfig.RuntimeConfig {
	now := time.Now()
	if snapshot := m.configSnapshot.Load(); robotConfigSnapshotFresh(snapshot, now) {
		return snapshot.effective
	}
	return m.refreshRobotConfig(now)
}

func robotConfigSnapshotFresh(snapshot *robotConfigSnapshot, now time.Time) bool {
	return snapshot != nil && !snapshot.checkedAt.IsZero() && now.Sub(snapshot.checkedAt) < robotConfigCheckInterval
}

func (m *RobotManager) refreshRobotConfig(now time.Time) robotconfig.RuntimeConfig {
	var out robotconfig.RuntimeConfig
	m.withCache("refresh_robot_config", func() {
		if snapshot := m.configSnapshot.Load(); robotConfigSnapshotFresh(snapshot, now) {
			out = snapshot.effective
			return
		}

		configPath := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
		configMod := fileModTime(configPath)
		if snapshot := m.configSnapshot.Load(); snapshot != nil && snapshot.modTime.Equal(configMod) {
			refreshed := &robotConfigSnapshot{
				base: snapshot.base, effective: snapshot.effective,
				modTime: snapshot.modTime, checkedAt: now,
			}
			m.configSnapshot.Store(refreshed)
			out = refreshed.effective
			return
		}

		rc, err := robotconfig.LoadFile(configPath)
		if err != nil {
			rc = robotconfig.Default()
		}
		robotconfig.Normalize(&rc)
		base := robotconfig.Clone(rc)
		effective := base
		m.policy().ApplyConfig(&effective, m.adaptiveSchedulerSignals())
		snapshot := &robotConfigSnapshot{base: base, effective: effective, modTime: configMod, checkedAt: now}
		m.configSnapshot.Store(snapshot)
		out = snapshot.effective
	})
	return out
}

func (m *RobotManager) refreshAdaptiveRobotConfig(signals adaptiveSchedulerSignals) (robotconfig.RuntimeConfig, schedulerPolicyDecision) {
	_ = m.loadRobotConfig()
	var out robotconfig.RuntimeConfig
	var decision schedulerPolicyDecision
	m.withCache("refresh_adaptive_robot_config", func() {
		snapshot := m.configSnapshot.Load()
		if snapshot == nil {
			return
		}
		effective := snapshot.base
		decision = m.policy().ApplyConfig(&effective, signals)
		updated := &robotConfigSnapshot{
			base: snapshot.base, effective: effective,
			modTime: snapshot.modTime, checkedAt: snapshot.checkedAt,
		}
		m.configSnapshot.Store(updated)
		out = effective
	})
	return out, decision
}

func (m *RobotManager) loadShoutTemplates() robottemplate.ShoutTemplates {
	if m.cfg == nil {
		return catalog.ShoutTemplates("")
	}
	return catalog.ShoutTemplates(m.cfg.ConfigDir)
}

func (m *RobotManager) loadNameTemplates() robottemplate.NameTemplates {
	if m.cfg == nil {
		return catalog.NameTemplates("")
	}
	return catalog.NameTemplates(m.cfg.ConfigDir)
}
