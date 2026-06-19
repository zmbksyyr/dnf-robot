package service

import (
	"fmt"
	"strings"
	"time"
)

var errOperationConflict = fmt.Errorf("structural operation already running")

func (m *RobotManager) BeginOperation(typ, scope string) RobotOperationStatus {
	op, _ := m.BeginOperationGuarded(typ, scope, false)
	return op
}

func (m *RobotManager) BeginOperationGuarded(typ, scope string, structural bool) (RobotOperationStatus, error) {
	now := time.Now()
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	typ = strings.TrimSpace(typ)
	scope = strings.TrimSpace(scope)
	if structural {
		for _, existing := range m.operations {
			if existing.State != "running" || !isStructuralOperation(existing.Type) {
				continue
			}
			return RobotOperationStatus{}, fmt.Errorf("%w: %s %s", errOperationConflict, existing.Type, existing.Scope)
		}
	}
	m.nextOperationID++
	op := RobotOperationStatus{
		ID:        m.nextOperationID,
		Type:      typ,
		Scope:     scope,
		State:     "running",
		StartedAt: now,
		UpdatedAt: now,
	}
	m.operations = append([]RobotOperationStatus{op}, m.operations...)
	if len(m.operations) > 20 {
		m.operations = m.operations[:20]
	}
	return op, nil
}

func (m *RobotManager) beginTrackedStructuralOperation(typ, scope string) (RobotOperationStatus, func(string, error) RobotOperationStatus, error) {
	op, err := m.BeginOperationGuarded(typ, scope, true)
	if err != nil {
		return RobotOperationStatus{}, nil, err
	}
	done := m.beginStructuralOp(typ)
	return op, func(summary string, opErr error) RobotOperationStatus {
		status := m.CompleteOperation(op.ID, summary, opErr)
		done()
		return status
	}, nil
}

func isStructuralOperation(typ string) bool {
	switch strings.TrimSpace(typ) {
	case "create", "cleanup", "createRobots", "cleanupRobots", "cleanupRobotsAsync":
		return true
	default:
		return false
	}
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
