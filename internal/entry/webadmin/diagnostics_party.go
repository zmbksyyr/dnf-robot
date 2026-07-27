package webadmin

import (
	"path/filepath"

	"robot/internal/capability/robotconfig"
)

func (b *diagnosticsBuilder) addPartySection() {
	cfg, err := b.server.loadPartyCompatConfig()
	if err != nil {
		b.addSection("Party", diagnosticsCheck{Name: "party compat config", Status: diagError, Message: err.Error()})
		return
	}
	status := inspectPartyCompat(b.cfg.RobotGamePort, cfg)
	status.DesiredEnabled = cfg.Enabled
	checks := []diagnosticsCheck{
		{
			Name:     "party compatibility patch",
			Status:   partyCompatDiagStatus(status),
			Message:  partyCompatDiagMessage(status),
			Expected: map[string]interface{}{"desired_enabled": cfg.Enabled, "account_start": cfg.AccountStart, "account_end": cfg.AccountEnd},
			Observed: status,
		},
		portDialCheck("relay service", "127.0.0.1", b.cfg.RelayPort),
		udpListeningCheck("party route0 UDP", b.cfg.PartyRoute0Port),
	}
	checks = append(checks, partyAccountRangeCheck(b.cfg.ConfigDir, status.AccountStart, status.AccountEnd))
	b.addSection("Party", checks...)
}

func partyAccountRangeCheck(configDir string, patchStart, patchEnd uint32) diagnosticsCheck {
	path := filepath.Join(configDir, "robot_config.ini")
	rc, err := robotconfig.LoadFile(path)
	if err != nil {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "cannot load robot account range: " + err.Error(),
			Expected: path,
		}
	}

	observed := map[string]interface{}{
		"robot_uid_start":     rc.RobotUIDStart,
		"robot_uid_end":       rc.RobotUIDEnd,
		"patch_start":         patchStart,
		"patch_end_exclusive": patchEnd,
	}
	expectedStart, expectedEnd, ok := partyCompatConfiguredWindow(rc.RobotUIDStart, rc.RobotUIDEnd)
	if !ok {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "configured robot account range is invalid",
			Observed: observed,
		}
	}
	if patchStart > expectedStart || patchEnd < expectedEnd {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "party patch range does not cover the configured party account window",
			Expected: map[string]interface{}{"patch_start_lte": expectedStart, "patch_end_exclusive_gte": expectedEnd},
			Observed: observed,
		}
	}
	return diagnosticsCheck{
		Name:     "party account range",
		Status:   diagOK,
		Message:  "party patch range covers the configured party account window",
		Observed: observed,
	}
}

func partyCompatDiagStatus(status partyCompatStatus) string {
	if status.DesiredEnabled && (!status.Enabled || status.State != "on") {
		if status.State == "unavailable" {
			return diagWarn
		}
		return diagError
	}
	if !status.DesiredEnabled && status.Enabled {
		return diagWarn
	}
	return diagOK
}

func partyCompatDiagMessage(status partyCompatStatus) string {
	if status.Message != "" {
		return status.Message
	}
	if status.DesiredEnabled && status.Enabled {
		return "party compatibility patch is active"
	}
	if status.DesiredEnabled {
		return "party compatibility patch is desired but not active"
	}
	if status.Enabled {
		return "party compatibility patch is active while desired off"
	}
	return "party compatibility patch is off"
}
