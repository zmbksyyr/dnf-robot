package tcpapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
