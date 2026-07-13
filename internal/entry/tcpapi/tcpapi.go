package tcpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"robot/internal/capability/marketapp"
	robotcap "robot/internal/capability/robot"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/scheduler"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
)

// ---- router.go ----
var asyncActions sync.Map
var marketApp *marketapp.App

func SetMarketApp(app *marketapp.App) {
	marketApp = app
}

func HandlePacket(clientID, pkt string, manager *scheduler.RobotManager) string {
	defer func() {
		if r := recover(); r != nil {
			logRobotActionf("[handlePacket] panic recovered client=%s err=%v\n", clientID, r)
		}
	}()
	cmd := extractTagContent(pkt, "c")
	if err := requireValidKeypair(cmd, manager); err != nil {
		return wrapResult(map[string]interface{}{"ok": false, "error": err.Error(), "result": manager.KeypairStatus()})
	}
	switch cmd {
	case "05":
		return ""
	case "sys":
		return wrapResult(map[string]interface{}{"ok": true, "message": "sys ok"})
	case "createRobots":
		var req robotcap.CreateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		robots, err := manager.CreateRobots(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "robots": robots})
	case "robotsOnline":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.OnlineManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsOnlineAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsOnlineAsync", robotcap.CommandRequestScope(req), func() (string, error) {
			res, err := manager.OnlineManaged(req)
			logRobotCommandResult("robotsOnlineAsync", res, err)
			return robotcap.CommandOperationSummary(res, err), err
		})
	case "robotsMove":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.MoveManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShout":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutBothManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShoutWorld":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutManaged(req, true)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsShoutLocal":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.ShoutManaged(req, false)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsStore":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.StoreManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsStoreAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsStoreAsync", robotcap.CommandRequestScope(req), func() (string, error) {
			res, err := manager.StoreManaged(req)
			logRobotCommandResult("robotsStoreAsync", res, err)
			return robotcap.CommandOperationSummary(res, err), err
		})
	case "robotsStatus":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.RobotsStatus(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "robotsLogout":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.LogoutManaged(req)
		return wrapResult(map[string]interface{}{"ok": err == nil && res.Failed == 0, "error": errString(err), "result": res})
	case "robotsLogoutAsync":
		req, err := parseRobotCommand(pkt)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueRobotAction(manager, "robotsLogoutAsync", robotcap.CommandRequestScope(req), func() (string, error) {
			res, err := manager.LogoutManaged(req)
			logRobotCommandResult("robotsLogoutAsync", res, err)
			return robotcap.CommandOperationSummary(res, err), err
		})
	case "autoStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.AutoStatus()})
	case "schedulerStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SchedulerStatus()})
	case "operationStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.OperationStatus()})
	case "systemStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SystemStatus()})
	case "systemAnnouncement":
		return monitorAnnouncement(pkt, manager, scheduler.SystemAnnouncementWebNoticeSingle)
	case "goroutineDump":
		return wrapResult(map[string]interface{}{"ok": true, "result": goroutineDump()})
	case "databaseStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.DatabaseStatus()})
	case "keypairStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.KeypairStatus()})
	case "keypairReleaseDefault":
		res, err := manager.ReleaseDefaultKeypair()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketStatus":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return wrapResult(map[string]interface{}{"ok": true, "result": app.Status()})
	case "marketStart":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.SetAutoEnabled(true)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketStop":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.SetAutoEnabled(false)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketConfigUpdate":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.ConfigUpdateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.UpdateConfig(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketRestockOnce":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.RestockRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		req.Execute = true
		res, err := app.RestockOnce(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketCollectOnce":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.CollectRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		req.Execute = true
		res, err := app.CollectOnce(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketSyncItemInfo":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res := app.SyncItemInfoDAT()
		return wrapResult(map[string]interface{}{"ok": res.Error == "", "error": res.Error, "result": res})
	case "marketPVFUpgradeSeparateStatus":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.PVFUpgradeSeparateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.PVFUpgradeSeparateStatus(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketPVFPatchUpgradeSeparate":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.PVFUpgradeSeparateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.PVFPatchUpgradeSeparate(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketClearSystemStock":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.ClearSystemMarketStock()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketInstallAuctionGuard":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.AuctionSearchGuardRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.InstallAuctionSearchGuard(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "marketPatchAuctionMemory":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		var req marketapp.AuctionMemoryPatchRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := app.PatchAuctionMemory(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "autoStart":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SetAutoEnabled(true)})
	case "autoStop":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SetAutoEnabled(false)})
	case "robotConfigGet":
		res, err := manager.RobotConfig()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "robotConfigUpdate":
		var req robotcap.ConfigUpdateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		res, err := manager.UpdateRobotConfig(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "cleanupRobots":
		var req robotcap.CleanupRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		if cleanupRequiresAsync(req) {
			return wrapResult(map[string]interface{}{"ok": false, "error": "full cleanup must use cleanupRobotsAsync"})
		}
		res, err := manager.CleanupRobots(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
	case "cleanupRobotsAsync":
		var req robotcap.CleanupRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
		}
		return queueExclusiveAction("cleanupRobotsAsync", func() {
			res, err := manager.CleanupRobots(req)
			if err != nil {
				logRobotActionf("[WebAction] cleanupRobotsAsync failed err=%v\n", err)
				return
			}
			logRobotActionf("[WebAction] cleanupRobotsAsync done candidates=%d deleted=%d skipped=%d\n",
				len(res.Candidates), res.Deleted, res.Skipped)
		})
	default:
		logRobotActionf("unknown command: %s\n", cmd)
		return wrapResult(map[string]interface{}{"ok": false, "error": "unknown command"})
	}
}

func cleanupRequiresAsync(req robotcap.CleanupRequest) bool {
	return req.Force && len(req.UIDs) == 0 && req.MinUID <= 0 && req.MaxUID <= 0
}

func goroutineDump() map[string]interface{} {
	var buf bytes.Buffer
	if prof := pprof.Lookup("goroutine"); prof != nil {
		_ = prof.WriteTo(&buf, 1)
	}
	return map[string]interface{}{
		"count": runtime.NumGoroutine(),
		"dump":  buf.String(),
	}
}

type monitorAnnouncementRequest struct {
	Message string `json:"message"`
}

func monitorAnnouncement(pkt string, manager *scheduler.RobotManager, kind string) string {
	var req monitorAnnouncementRequest
	if err := decodePayload(pkt, &req); err != nil {
		return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()})
	}
	res, err := manager.MonitorAnnouncement(kind, req.Message)
	return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res})
}

func requireValidKeypair(cmd string, manager *scheduler.RobotManager) error {
	if !RequiresValidKeypair(cmd) {
		return nil
	}
	st := manager.KeypairStatus()
	if st.GameValid {
		return nil
	}
	if st.Error != "" {
		return fmt.Errorf("RSA key unavailable: %s", st.Error)
	}
	if st.KeyReason != "" {
		return fmt.Errorf("RSA key unavailable: %s", st.KeyReason)
	}
	return fmt.Errorf("RSA key unavailable")
}

func RequiresValidKeypair(cmd string) bool {
	switch cmd {
	case "createRobots",
		"robotsOnline",
		"robotsOnlineAsync",
		"robotsMove",
		"robotsShout",
		"robotsShoutWorld",
		"robotsShoutLocal",
		"robotsStore",
		"robotsStoreAsync",
		"robotsLogout",
		"robotsLogoutAsync",
		"autoStart":
		return true
	default:
		return false
	}
}

func queueRobotAction(manager *scheduler.RobotManager, name, scope string, fn func() (string, error)) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	op := manager.BeginOperation(name, scope)
	go func() {
		defer asyncActions.Delete(name)
		summary, err := fn()
		manager.CompleteOperation(op.ID, summary, err)
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued", "operation": op}})
}

func queueExclusiveAction(name string, fn func()) string {
	if _, loaded := asyncActions.LoadOrStore(name, true); loaded {
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "running"}})
	}
	go func() {
		defer asyncActions.Delete(name)
		fn()
	}()
	return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{"state": "queued"}})
}

func logRobotCommandResult(name string, res robotcap.CommandResult, err error) {
	if err != nil {
		logRobotActionf("[WebAction] %s failed err=%v\n", name, err)
		return
	}
	logRobotActionf("[WebAction] %s done requested=%d accepted=%d confirmed=%d failed=%d\n",
		name, res.Requested, res.Accepted, res.Confirmed, res.Failed)
}

func logRobotActionf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}

func parseRobotCommand(pkt string) (robotcap.CommandRequest, error) {
	var req robotcap.CommandRequest
	if err := decodePayload(pkt, &req); err != nil {
		return req, err
	}
	return req, nil
}

func decodePayload(pkt string, dst interface{}) error {
	payload := strings.TrimSpace(extractPayload(pkt))
	if payload == "" {
		payload = "{}"
	}
	if err := json.Unmarshal([]byte(payload), dst); err != nil {
		return fmt.Errorf("invalid json payload: %w", err)
	}
	return nil
}

func extractPayload(pkt string) string {
	if v := extractTagContent(pkt, "json"); v != "" {
		return v
	}
	if v := extractTagContent(pkt, "key"); v != "" {
		return v
	}
	return "{}"
}

func wrapResult(v interface{}) string {
	data, _ := json.Marshal(v)
	return "<tw><result>" + string(data) + "</result></tw>"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func requireMarketApp() (*marketapp.App, error) {
	if marketApp == nil {
		return nil, fmt.Errorf("market app is not initialized")
	}
	return marketApp, nil
}

func extractTagContent(pkt, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(pkt, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(pkt[start:], closeTag)
	if end < 0 {
		return ""
	}
	return pkt[start : start+end]
}
