package tcpapi

import (
	"bytes"
	"runtime"
	"runtime/pprof"

	"robot/internal/scheduler"
)

func handleSystemCommand(cmd, pkt string, manager *scheduler.RobotManager) (string, bool) {
	switch cmd {
	case "autoStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.AutoStatus()}), true
	case "schedulerStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SchedulerStatus()}), true
	case "operationStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.OperationStatus()}), true
	case "systemStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.SystemStatus()}), true
	case "systemAnnouncement":
		return monitorAnnouncement(pkt, manager, scheduler.SystemAnnouncementWebNoticeSingle), true
	case "goroutineDump":
		return wrapResult(map[string]interface{}{"ok": true, "result": goroutineDump()}), true
	case "databaseStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.DatabaseStatus()}), true
	case "keypairStatus":
		return wrapResult(map[string]interface{}{"ok": true, "result": manager.KeypairStatus()}), true
	case "keypairReleaseDefault":
		res, err := manager.ReleaseDefaultKeypair()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	default:
		return "", false
	}
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
