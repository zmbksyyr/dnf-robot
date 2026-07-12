package marketapp

import (
	"fmt"
	"strings"
	"testing"
)

func TestExecuteActionsAllowsCeraSuccessWithoutAuctionID(t *testing.T) {
	ok := true
	app := testApp(t)
	app.executors = fixedActionExecutorFactory{result: ActionExecutionResult{ResultOK: &ok}}
	job := &JobSummary{}

	failed, entries, err := app.executeActions("test", []Action{{
		Market: marketNameCera,
		ItemID: 2675345,
	}}, 1, true, job)
	if err != nil || failed != 0 || len(entries) != 1 || !entries[0].OK {
		t.Fatalf("cera action failed=%d err=%v entries=%#v", failed, err, entries)
	}
}

func TestExecuteActionsUsesGenericReasonForCeraRejectWithoutAuctionID(t *testing.T) {
	ok := false
	app := testApp(t)
	app.executors = fixedActionExecutorFactory{result: ActionExecutionResult{ResultOK: &ok}}
	job := &JobSummary{}

	failed, entries, err := app.executeActions("test", []Action{{
		Market: marketNameCera,
		ItemID: 2675345,
	}}, 1, true, job)
	if err == nil || failed != 1 || len(entries) != 1 || entries[0].OK {
		t.Fatalf("cera reject failed=%d err=%v entries=%#v", failed, err, entries)
	}
	if !strings.Contains(err.Error(), "reason=rejected") {
		t.Fatalf("cera reject reason should be generic rejected, got %v", err)
	}
	if reason := actionLogReason(entries[0], nil); reason != "rejected" {
		t.Fatalf("cera action reason = %q, want rejected", reason)
	}
	if len(app.auctionRejected) != 0 {
		t.Fatalf("cera reject should not touch auction rejected queue: %#v", app.auctionRejected)
	}
}

func TestExecuteActionsRequiresAuctionIDForAuctionRegister(t *testing.T) {
	ok := true
	app := testApp(t)
	app.auctionQueue = []uint32{1001, 1002}
	app.auctionSpecialQueue = []uint32{1003}
	app.executors = fixedActionExecutorFactory{result: ActionExecutionResult{ResultOK: &ok}}
	job := &JobSummary{}

	failed, entries, err := app.executeActions("test", []Action{{
		Market: marketNameAuction,
		ItemID: 1001,
	}}, 1, true, job)
	if err == nil || failed != 1 || len(entries) != 1 || entries[0].OK {
		t.Fatalf("auction action without id failed=%d err=%v entries=%#v", failed, err, entries)
	}
	if queueContains(app.auctionQueue, 1001) || queueContains(app.auctionSpecialQueue, 1001) || !queueContains(app.auctionRejected, 1001) {
		t.Fatalf("failed auction register should be rejected; normal=%v special=%v rejected=%v", app.auctionQueue, app.auctionSpecialQueue, app.auctionRejected)
	}
	if state := app.auctionRejectedMeta[1001]; state.Reason != "missing_auction_id" || state.Count != 1 {
		t.Fatalf("rejected meta = %#v", state)
	}
}

func TestExecuteActionsRejectsAuctionRegisterOnExecutorError(t *testing.T) {
	app := testApp(t)
	app.auctionQueue = []uint32{1001}
	app.executors = fixedActionExecutorFactory{err: fmt.Errorf("register timeout")}
	job := &JobSummary{}

	failed, entries, err := app.executeActions("test", []Action{{
		Market: marketNameAuction,
		ItemID: 1001,
	}}, 1, true, job)
	if err == nil || failed != 1 || len(entries) != 1 || entries[0].OK {
		t.Fatalf("auction executor error failed=%d err=%v entries=%#v", failed, err, entries)
	}
	if queueContains(app.auctionQueue, 1001) || !queueContains(app.auctionRejected, 1001) {
		t.Fatalf("executor error should reject auction item; normal=%v rejected=%v", app.auctionQueue, app.auctionRejected)
	}
	if state := app.auctionRejectedMeta[1001]; state.Reason != "executor_error" || state.Count != 1 {
		t.Fatalf("rejected meta = %#v", state)
	}
}

type fixedActionExecutorFactory struct {
	result ActionExecutionResult
	err    error
}

func (f fixedActionExecutorFactory) NewActionExecutor(Config) ActionExecutor {
	return fixedActionExecutor{result: f.result, err: f.err}
}

type fixedActionExecutor struct {
	result ActionExecutionResult
	err    error
}

func (e fixedActionExecutor) Execute(Action) (ActionExecutionResult, error) {
	return e.result, e.err
}

func (e fixedActionExecutor) Close() {}
