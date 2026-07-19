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
		m.configCached = false
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
	return robotcap.ConfigResult{Path: path, Text: robotconfig.PublicText(string(data)), Config: m.loadRobotConfig()}, nil
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

func (m *RobotManager) loadRobotConfig() robotconfig.RuntimeConfig {
	configPath := filepath.Join(m.cfg.ConfigDir, "robot_config.ini")
	configMod := fileModTime(configPath)
	var out robotconfig.RuntimeConfig
	m.withCache("load_robot_config_read", func() {
		if m.configCached && m.configMod.Equal(configMod) {
			out = robotconfig.Clone(m.configCache)
			return
		}

		rc, err := robotconfig.LoadFile(configPath)
		if err != nil {
			rc = robotconfig.Default()
		}
		applyAdaptiveSchedulerConfig(&rc, m.adaptiveSchedulerSignals())
		robotconfig.Normalize(&rc)
		m.configCache = robotconfig.Clone(rc)
		m.configMod = configMod
		m.configCached = true
		out = robotconfig.Clone(rc)
	})
	return out
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
