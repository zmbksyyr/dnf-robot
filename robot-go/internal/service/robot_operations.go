package service

import (
	"fmt"
	"strings"
	"time"
)

func (m *RobotManager) BeginOperation(typ, scope string) RobotOperationStatus {
	now := time.Now()
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.nextOperationID++
	op := RobotOperationStatus{
		ID:        m.nextOperationID,
		Type:      strings.TrimSpace(typ),
		Scope:     strings.TrimSpace(scope),
		State:     "running",
		StartedAt: now,
		UpdatedAt: now,
	}
	m.operations = append([]RobotOperationStatus{op}, m.operations...)
	if len(m.operations) > 20 {
		m.operations = m.operations[:20]
	}
	return op
}

func (m *RobotManager) CompleteOperation(id int64, summary string, err error) RobotOperationStatus {
	now := time.Now()
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	for i := range m.operations {
		if m.operations[i].ID != id {
			continue
		}
		m.operations[i].UpdatedAt = now
		m.operations[i].FinishedAt = now
		m.operations[i].Summary = strings.TrimSpace(summary)
		if err != nil {
			m.operations[i].State = "failed"
			m.operations[i].Error = err.Error()
		} else {
			m.operations[i].State = "done"
		}
		return m.operations[i]
	}
	op := RobotOperationStatus{ID: id, State: "unknown", Summary: strings.TrimSpace(summary), UpdatedAt: now}
	if err != nil {
		op.State = "failed"
		op.Error = err.Error()
	}
	return op
}

func (m *RobotManager) RecentOperation() RobotOperationStatus {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if len(m.operations) == 0 {
		return RobotOperationStatus{}
	}
	return m.operations[0]
}

func (m *RobotManager) OperationStatus() []RobotOperationStatus {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	out := make([]RobotOperationStatus, len(m.operations))
	copy(out, m.operations)
	return out
}

func CommandOperationSummary(res RobotCommandResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("requested=%d accepted=%d confirmed=%d failed=%d", res.Requested, res.Accepted, res.Confirmed, res.Failed)
}

func CleanupOperationSummary(res RobotCleanupResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("candidates=%d deleted=%d skipped=%d", len(res.Candidates), res.Deleted, res.Skipped)
}
