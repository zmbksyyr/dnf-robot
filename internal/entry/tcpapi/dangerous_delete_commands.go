package tcpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	robotcap "robot/internal/capability/robot"
	"robot/internal/scheduler"
	"strings"
	"sync"
	"time"
)

const dangerousDeleteUnlockCode = "123"

type dangerousDeleteUnlockRequest struct {
	Code string `json:"code"`
}

type dangerousDeleteCommandRequest struct {
	Token  string `json:"token"`
	Mode   string `json:"mode"`
	CID    int    `json:"cid"`
	UID    int    `json:"uid"`
	MinUID int    `json:"uid_min"`
	MaxUID int    `json:"uid_max"`
}

var dangerousDeleteTokens sync.Map

type dangerousDeleteToken struct {
	Expires  time.Time
	ClientIP string
}

func handleDangerousDeleteCommand(clientID, cmd, pkt string, manager *scheduler.RobotManager) (string, bool) {
	switch cmd {
	case "dangerousDeleteUnlock":
		clientIP, ok := loopbackClientIP(clientID)
		if !ok {
			return wrapResult(map[string]interface{}{"ok": false, "error": "dangerous delete is only available through the local web admin"}), true
		}
		var req dangerousDeleteUnlockRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		if strings.TrimSpace(req.Code) != dangerousDeleteUnlockCode {
			return wrapResult(map[string]interface{}{"ok": false, "error": "invalid unlock code"}), true
		}
		token, err := issueDangerousDeleteToken(clientIP)
		if err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		uidStart, uidEnd := manager.DangerousDeleteDefaults()
		return wrapResult(map[string]interface{}{"ok": true, "result": map[string]interface{}{
			"token": token, "uid_min": uidStart, "uid_max": uidEnd,
		}}), true
	case "dangerousDeleteAsync":
		clientIP, ok := loopbackClientIP(clientID)
		if !ok {
			return wrapResult(map[string]interface{}{"ok": false, "error": "dangerous delete is only available through the local web admin"}), true
		}
		var req dangerousDeleteCommandRequest
		if err := decodePayload(pkt, &req); err != nil {
			return wrapResult(map[string]interface{}{"ok": false, "error": err.Error()}), true
		}
		if !consumeDangerousDeleteToken(req.Token, clientIP, time.Now()) {
			return wrapResult(map[string]interface{}{"ok": false, "error": "dangerous delete is not unlocked or token expired"}), true
		}
		deleteReq := robotcap.DangerousDeleteRequest{Mode: req.Mode, CID: req.CID, UID: req.UID, MinUID: req.MinUID, MaxUID: req.MaxUID}
		return queueExclusiveAction("dangerousDeleteAsync", func() {
			res, err := manager.DangerousDelete(deleteReq)
			if err != nil {
				logRobotActionf("[WebAction] dangerousDeleteAsync failed mode=%s err=%v\n", req.Mode, err)
				return
			}
			logRobotActionf("[WebAction] dangerousDeleteAsync done mode=%s accounts=%d characters=%d registry=%d\n",
				res.Mode, res.AccountCount, res.CharacterCount, res.RegistryCount)
		}), true
	default:
		return "", false
	}
}

func issueDangerousDeleteToken(clientIP string) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate dangerous delete token: %w", err)
	}
	token := hex.EncodeToString(raw)
	dangerousDeleteTokens.Store(token, dangerousDeleteToken{
		Expires:  time.Now().Add(10 * time.Minute),
		ClientIP: clientIP,
	})
	return token, nil
}

func consumeDangerousDeleteToken(token, clientIP string, now time.Time) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	raw, ok := dangerousDeleteTokens.LoadAndDelete(token)
	if !ok {
		return false
	}
	issued, ok := raw.(dangerousDeleteToken)
	return ok && issued.ClientIP == clientIP && now.Before(issued.Expires)
}

func loopbackClientIP(clientID string) (string, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(clientID))
	if err != nil {
		return "", false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", false
	}
	return ip.String(), true
}
