package tcpapi

import (
	"fmt"
	"robot/internal/capability/marketapp"
)

var marketApp *marketapp.App

func SetMarketApp(app *marketapp.App) {
	marketApp = app
}

func handleMarketCommand(cmd, pkt string) (string, bool) {
	switch cmd {
	case "marketStatus":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		return wrapResult(map[string]interface{}{"ok": true, "result": app.Status()}), true
	case "marketStart":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.SetAutoEnabled(true)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketStop":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.SetAutoEnabled(false)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketConfigUpdate":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.ConfigUpdateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.UpdateConfig(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketRestockOnce":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.RestockRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		req.Execute = true
		res, err := app.RestockOnce(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketCollectOnce":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.CollectRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		req.Execute = true
		res, err := app.CollectOnce(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketSyncItemInfo":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res := app.SyncItemInfoDAT()
		return wrapResult(map[string]interface{}{"ok": res.Error == "", "error": res.Error, "result": res}), true
	case "marketPVFUpgradeSeparateStatus":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.PVFUpgradeSeparateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.PVFUpgradeSeparateStatus(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketPVFPatchUpgradeSeparate":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.PVFUpgradeSeparateRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.PVFPatchUpgradeSeparate(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketClearSystemStock":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.ClearSystemMarketStock()
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketInstallAuctionGuard":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.AuctionSearchGuardRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.InstallAuctionSearchGuard(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	case "marketPatchAuctionMemory":
		app, err := requireMarketApp()
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		var req marketapp.AuctionMemoryPatchRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		res, err := app.PatchAuctionMemory(req)
		return wrapResult(map[string]interface{}{"ok": err == nil, "error": errString(err), "result": res}), true
	default:
		return "", false
	}
}

func requireMarketApp() (*marketapp.App, error) {
	if marketApp == nil {
		return nil, fmt.Errorf("market app is not initialized")
	}
	return marketApp, nil
}
