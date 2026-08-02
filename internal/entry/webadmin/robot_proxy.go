package webadmin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	foundationconfig "robot/internal/foundation/config"
	foundationnetwork "robot/internal/foundation/network"
)

type callRequest struct {
	Command string                 `json:"command"`
	Payload map[string]interface{} `json:"payload"`
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req callRequest
	if err := foundationconfig.DecodeJSONLimit(r.Body, 2*1024*1024, &req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "empty command"})
		return
	}
	raw, err := callRobot(s.robotAddr, req.Command, req.Payload, robotCallTimeout(req.Command), s.cfg.MaxResponseBytes)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	out := map[string]interface{}{"ok": true, "result": parseRobotResult(raw)}
	if r.URL.Query().Get("raw") == "1" {
		out["raw"] = raw
	}
	writeJSON(w, out)
}

func robotCallTimeout(command string) time.Duration {
	switch strings.TrimSpace(command) {
	case "robotsStore":
		return 90 * time.Second
	default:
		return 30 * time.Second
	}
}

func callRobot(addr, command string, payload map[string]interface{}, timeout time.Duration, maxResponseBytes int) (string, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if maxResponseBytes <= 0 {
		maxResponseBytes = 4 * 1024 * 1024
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	packet := fmt.Sprintf("<tw><c>%s</c><json>%s</json></tw>", command, body)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := foundationnetwork.WriteFull(conn, []byte(packet)); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	endTag := []byte("</tw>")
	searchFrom := 0
	tmp := make([]byte, 64*1024)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if buf.Len() > maxResponseBytes {
				return "", fmt.Errorf("robot response too large")
			}
			data := buf.Bytes()
			if bytes.Index(data[searchFrom:], endTag) >= 0 {
				return buf.String(), nil
			}
			searchFrom = len(data) - len(endTag) + 1
			if searchFrom < 0 {
				searchFrom = 0
			}
		}
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return buf.String(), nil
			}
			return "", err
		}
	}
}

func parseRobotResult(raw string) interface{} {
	startTag := "<result>"
	endTag := "</result>"
	start := strings.Index(raw, startTag)
	if start < 0 {
		return nil
	}
	start += len(startTag)
	end := strings.Index(raw[start:], endTag)
	if end < 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal([]byte(raw[start:start+end]), &out); err != nil {
		return nil
	}
	return out
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	reportResponseWriteError(w, json.NewEncoder(w).Encode(v))
}
