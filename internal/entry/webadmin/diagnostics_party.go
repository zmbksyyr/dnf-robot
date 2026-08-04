package webadmin

import (
	"fmt"
	"strconv"
	"strings"

	"robot/internal/foundation/config"
	"robot/internal/scheduler/repository"
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
		partyNetworkConfigCheck(b.cfg),
		{
			Name:     "party compatibility patch",
			Status:   partyCompatDiagStatus(status),
			Message:  partyCompatDiagMessage(status),
			Expected: map[string]interface{}{"desired_enabled": cfg.Enabled, "account_start": cfg.AccountStart, "account_end": cfg.AccountEnd},
			Observed: status,
		},
		portDialCheck("relay service", b.cfg.RelayHost, b.cfg.RelayPort),
		udpListeningCheck("party route0 UDP", b.cfg.PartyRoute0Port),
	}
	uids, uidErr := repository.ReadRobotRegistryUIDs(b.cfg)
	checks = append(checks, partyAccountRangeCheck(uids, uidErr, status.AccountStart, status.AccountEnd))
	b.addSection("Party", checks...)
}

func partyNetworkConfigCheck(cfg *config.SysConfig) diagnosticsCheck {
	if cfg == nil {
		return diagnosticsCheck{Name: "party network config", Status: diagError, Message: "system config is unavailable"}
	}
	source := "config"
	if strings.EqualFold(cfg.RobotConnectIPSetting, "auto") {
		source = "auto"
	}
	message := fmt.Sprintf("GAME=%s:%d source=%s LOGIN_IP=%s RELAY=%s:%d PARTY_MTU=auto<=1472",
		cfg.RobotConnectIP, cfg.RobotGamePort, source, cfg.RobotInnerIP, cfg.RelayHost, cfg.RelayPort)
	return diagnosticsCheck{
		Name:    "party network config",
		Status:  diagOK,
		Message: message,
		Observed: map[string]interface{}{
			"game_host_setting":  cfg.RobotConnectIPSetting,
			"game_host_resolved": cfg.RobotConnectIP,
			"login_ip":           cfg.RobotInnerIP,
			"relay_host":         cfg.RelayHost,
			"relay_port":         cfg.RelayPort,
			"party_mtu":          "auto<=1472",
		},
	}
}

func partyAccountRangeCheck(uids []uint32, loadErr error, patchStart, patchEnd uint32) diagnosticsCheck {
	observed := map[string]interface{}{
		"patch_start":         patchStart,
		"patch_end_exclusive": patchEnd,
		"robot_total":         len(uids),
	}
	if loadErr != nil {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  "cannot load robot UIDs from d_starsky.robot_registry: " + loadErr.Error(),
			Expected: "all robot_registry.uid values are inside [patch_start, patch_end_exclusive)",
			Observed: observed,
		}
	}
	if len(uids) == 0 {
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagWarn,
			Message:  "robot_registry is empty; party patch range coverage cannot be verified",
			Observed: observed,
		}
	}

	outside := make([]uint32, 0)
	for _, uid := range uids {
		if uid < patchStart || uid >= patchEnd {
			outside = append(outside, uid)
		}
	}
	observed["inside_count"] = len(uids) - len(outside)
	observed["outside_count"] = len(outside)
	if len(outside) > 0 {
		observed["outside_uids"] = outside
		return diagnosticsCheck{
			Name:     "party account range",
			Status:   diagError,
			Message:  fmt.Sprintf("%d of %d robot UIDs are outside party patch range [%d,%d): %s", len(outside), len(uids), patchStart, patchEnd, summarizePartyUIDs(outside, 10)),
			Expected: "all robot_registry.uid values are inside [patch_start, patch_end_exclusive)",
			Observed: observed,
		}
	}
	return diagnosticsCheck{
		Name:     "party account range",
		Status:   diagOK,
		Message:  fmt.Sprintf("all %d robot UIDs are inside party patch range [%d,%d)", len(uids), patchStart, patchEnd),
		Observed: observed,
	}
}

func summarizePartyUIDs(uids []uint32, limit int) string {
	if limit <= 0 || limit > len(uids) {
		limit = len(uids)
	}
	values := make([]string, 0, limit)
	for _, uid := range uids[:limit] {
		values = append(values, strconv.FormatUint(uint64(uid), 10))
	}
	result := strings.Join(values, ",")
	if limit < len(uids) {
		result += fmt.Sprintf(" ... and %d more", len(uids)-limit)
	}
	return result
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
