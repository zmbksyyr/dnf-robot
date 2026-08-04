package shared

type PartyDebugStatus struct {
	State       string   `json:"state"`
	StartedAt   string   `json:"started_at,omitempty"`
	StoppedAt   string   `json:"stopped_at,omitempty"`
	ElapsedMS   int64    `json:"elapsed_ms"`
	BytesUsed   int64    `json:"bytes_used"`
	LimitBytes  int64    `json:"limit_bytes"`
	EventCount  int      `json:"event_count"`
	Dropped     uint64   `json:"dropped"`
	StopReason  string   `json:"stop_reason,omitempty"`
	ReportLines []string `json:"report_lines,omitempty"`
}
