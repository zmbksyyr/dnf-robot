package service

import (
	"errors"
	"testing"
	"time"
)

var errTestOperation = errors.New("operation failed")

func TestRobotOperationsTrackRecentStatus(t *testing.T) {
	m := &RobotManager{}
	op := m.BeginOperation("cleanup", "uids=2")
	if op.ID != 1 || op.State != "running" || op.Type != "cleanup" {
		t.Fatalf("begin op got id=%d state=%s type=%s", op.ID, op.State, op.Type)
	}
	done := m.CompleteOperation(op.ID, "deleted=2", nil)
	if done.State != "done" || done.Summary != "deleted=2" || done.FinishedAt.IsZero() {
		t.Fatalf("done op got state=%s summary=%q finished=%s", done.State, done.Summary, done.FinishedAt)
	}
	recent := m.RecentOperation()
	if recent.ID != op.ID || recent.State != "done" {
		t.Fatalf("recent op got id=%d state=%s", recent.ID, recent.State)
	}
	failed := m.BeginOperation("online", "count=1")
	m.CompleteOperation(failed.ID, "", errTestOperation)
	recent = m.RecentOperation()
	if recent.ID != failed.ID || recent.State != "failed" || recent.Error == "" {
		t.Fatalf("failed recent got id=%d state=%s err=%q", recent.ID, recent.State, recent.Error)
	}
}

func TestStructuralOperationGuardRejectsOverlap(t *testing.T) {
	m := &RobotManager{}
	first, err := m.BeginOperationGuarded("cleanup", "all", true)
	if err != nil {
		t.Fatalf("first structural op failed: %v", err)
	}
	if _, err := m.BeginOperationGuarded("create", "count=1", true); err == nil {
		t.Fatalf("second structural op should conflict")
	}
	m.CompleteOperation(first.ID, "done", nil)
	if _, err := m.BeginOperationGuarded("create", "count=1", true); err != nil {
		t.Fatalf("structural op after completion should pass: %v", err)
	}
}

func TestStructuralOperationState(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	done := m.beginStructuralOp("cleanup")
	op, started, active := m.structuralOperation()
	if !active || op != "cleanup" || started.IsZero() {
		t.Fatalf("structural op got active=%v op=%q started=%s", active, op, started)
	}
	done()
	if op, _, active := m.structuralOperation(); active || op != "" {
		t.Fatalf("structural op should clear, active=%v op=%q", active, op)
	}
}

func TestStructuralOperationExpiresStaleState(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.autoMu.Lock()
	m.structuralOp = "cleanup"
	m.structuralOpStarted = time.Now().Add(-11 * time.Minute)
	m.autoMu.Unlock()

	if op, _, active := m.structuralOperation(); active || op != "" {
		t.Fatalf("stale structural op should expire, active=%v op=%q", active, op)
	}
}

func TestTrackedStructuralOperationMaintainsBothStates(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	op, finish, err := m.beginTrackedStructuralOperation("cleanup", "uids=2")
	if err != nil {
		t.Fatalf("begin tracked structural op failed: %v", err)
	}
	if op.ID == 0 || op.State != "running" {
		t.Fatalf("operation got id=%d state=%s", op.ID, op.State)
	}
	if activeOp, _, active := m.structuralOperation(); !active || activeOp != "cleanup" {
		t.Fatalf("structural op got active=%v op=%q", active, activeOp)
	}
	done := finish("deleted=2", nil)
	if done.State != "done" || done.Summary != "deleted=2" {
		t.Fatalf("finished op got state=%s summary=%q", done.State, done.Summary)
	}
	if activeOp, _, active := m.structuralOperation(); active || activeOp != "" {
		t.Fatalf("structural op should clear, active=%v op=%q", active, activeOp)
	}
}
