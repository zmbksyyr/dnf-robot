package marketapp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarketDecisionFieldOrderIsStable(t *testing.T) {
	text := (marketDecisionSnapshot{}).String()
	wantPrefix := "auction=false cera=false pvf_ready=false pvf_items=0 iteminfo_ids=0"
	if !strings.HasPrefix(text, wantPrefix) {
		t.Fatalf("decision field order changed: %s", text)
	}
	for _, want := range []string{"iteminfo_error=\"\"", "budget_normal=0", "selected_normal=0", "effective_max_actions=0", "max_concurrent=0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("decision log missing %q in %s", want, text)
		}
	}
}

func TestMarketPolicyRebuildsQueueAfterRepeatedZeroKinds(t *testing.T) {
	app := testApp(t)
	app.configDir = t.TempDir()
	restarts := 0
	app.restarter = func(name, reason string) {
		if name != marketServiceNameAuction || !strings.Contains(reason, "zero") {
			t.Fatalf("unexpected restart name=%q reason=%q", name, reason)
		}
		restarts++
	}
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {},
	}}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionRejectedTick = 7
	app.auctionQueueSource = "pvf_iteminfo"

	first := app.marketAutoPolicy("auction", app.cfg.Auto)
	if first.MaxActions != app.cfg.Auto.MaxActions || first.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("first policy changed pressure: %#v", first)
	}
	if len(app.auctionQueue) == 0 || len(app.auctionRejected) == 0 {
		t.Fatalf("first zero round should only observe queues: normal=%v rejected=%v", app.auctionQueue, app.auctionRejected)
	}

	second := app.marketAutoPolicy("auction", app.cfg.Auto)
	if second.MaxActions != app.cfg.Auto.MaxActions || second.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("second policy should rebuild only: %#v", second)
	}
	if len(app.auctionQueue) != 0 || len(app.auctionSpecialQueue) != 0 || len(app.auctionRejected) != 0 || app.auctionRejectedTick != 0 || app.auctionQueueSource != "" {
		t.Fatalf("second zero round did not reset queues: normal=%v special=%v rejected=%v tick=%d source=%q", app.auctionQueue, app.auctionSpecialQueue, app.auctionRejected, app.auctionRejectedTick, app.auctionQueueSource)
	}
	if restarts != 1 {
		t.Fatalf("second zero round restarts=%d, want 1", restarts)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != 2000 || third.MaxConcurrent != 4 {
		t.Fatalf("third zero round pressure = %#v, want 2000/4", third)
	}
	if restarts != 1 {
		t.Fatalf("third zero round should not restart again, restarts=%d", restarts)
	}
	status := app.policy["auction"]
	if status.Mode != marketPolicyModeDegraded || status.ZeroKindRounds != 3 {
		t.Fatalf("policy status = %#v, want degraded round 3", status)
	}
}

func TestMarketPolicyTracksZeroAuctionCandidates(t *testing.T) {
	dir := t.TempDir()
	app := testApp(t)
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {10075: 1},
	}}
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 10075, "price": 100, "attach": "trade", "slot": "weapon"},
	})
	mustWriteText(t, filepath.Join(dir, "iteminfo.dat"), "99999 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n")
	app.auctionQueue = []uint32{10075}
	app.auctionQueueSource = "pvf_iteminfo"

	first := app.marketAutoPolicy("auction", app.cfg.Auto)
	if first.MaxActions != app.cfg.Auto.MaxActions || first.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("first zero candidate policy changed pressure: %#v", first)
	}

	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	if len(app.auctionQueue) != 0 || app.auctionQueueSource != "" {
		t.Fatalf("second zero candidate round did not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != 2000 || third.MaxConcurrent != 4 {
		t.Fatalf("third zero candidate pressure = %#v, want 2000/4", third)
	}
	status := app.policy["auction"]
	if status.ZeroCandidateRounds != 3 || status.Candidates != 0 || status.CandidateSource == "" {
		t.Fatalf("candidate policy status = %#v", status)
	}
}

func TestMarketPolicyDetectsStagnantAuctionGrowth(t *testing.T) {
	dir := t.TempDir()
	app := testApp(t)
	app.configDir = dir
	app.cfg.ItemInfoTargets = []string{filepath.Join(dir, "iteminfo.dat")}
	stock := map[uint32]int{10075: 1}
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: stock,
	}}
	mustWriteJSON(t, filepath.Join(dir, "pvf_stackable_catalog.json"), []map[string]interface{}{})
	mustWriteJSON(t, filepath.Join(dir, "pvf_equipment_catalog.json"), []map[string]interface{}{
		{"id": 10075, "price": 100, "attach": "trade", "slot": "weapon"},
		{"id": 10076, "price": 100, "attach": "trade", "slot": "weapon"},
		{"id": 10077, "price": 100, "attach": "trade", "slot": "weapon"},
	})
	mustWriteText(t, filepath.Join(dir, "iteminfo.dat"), ""+
		"10075 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n"+
		"10076 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n"+
		"10077 0 1 1 1 1 1 1 1 1 1 1 1 1 `x` `x` 1\n")
	app.auctionQueue = []uint32{10076}
	app.auctionQueueSource = "pvf_iteminfo"

	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	second := app.marketAutoPolicy("auction", app.cfg.Auto)
	if second.MaxActions != app.cfg.Auto.MaxActions || second.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("second stagnant round should observe only: %#v", second)
	}
	if len(app.auctionQueue) == 0 || app.auctionQueueSource == "" {
		t.Fatalf("first comparable stagnant round should not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	third := app.marketAutoPolicy("auction", app.cfg.Auto)
	if third.MaxActions != app.cfg.Auto.MaxActions || third.MaxConcurrent != app.cfg.Auto.MaxConcurrent {
		t.Fatalf("third stagnant round should rebuild only: %#v", third)
	}
	if len(app.auctionQueue) != 0 || app.auctionQueueSource != "" {
		t.Fatalf("stagnant recovery did not reset queue: queue=%v source=%q", app.auctionQueue, app.auctionQueueSource)
	}

	fourth := app.marketAutoPolicy("auction", app.cfg.Auto)
	if fourth.MaxActions != 2000 || fourth.MaxConcurrent != 4 {
		t.Fatalf("fourth stagnant pressure = %#v, want 2000/4", fourth)
	}
	status := app.policy["auction"]
	if status.StagnantRounds != 3 || status.Candidates != 3 || status.DBKinds != 1 {
		t.Fatalf("stagnant status = %#v", status)
	}

	stock[10076] = 1
	_ = app.marketAutoPolicy("auction", app.cfg.Auto)
	status = app.policy["auction"]
	if status.StagnantRounds != 0 || status.KindDelta <= 0 {
		t.Fatalf("growth did not reset stagnant status: %#v", status)
	}
}

func TestMarketPolicyBlockedStateRecordsReason(t *testing.T) {
	app := testApp(t)
	app.configDir = t.TempDir()
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	app.auctionRejectedMeta = map[uint32]auctionRejectedState{30075: {Reason: "executor_error", Count: 1, First: time.Now(), Last: time.Now()}}
	app.auctionRejectedTick = 4
	app.auctionQueueSource = "pvf_iteminfo"

	app.markMarketPolicyBlocked("auction", "df_game_r is not running")
	status := app.policy["auction"]
	if status.Mode != marketPolicyModeDegraded || status.Reason != "df_game_r is not running" {
		t.Fatalf("blocked status = %#v", status)
	}
	if status.QueueNormal != 1 || status.QueueRejected != 1 || status.QueueSource != "pvf_iteminfo" {
		t.Fatalf("blocked queue snapshot = %#v", status)
	}
	if status.QueueRejectedTracked != 1 || status.QueueRejectedRetryIn != 6 {
		t.Fatalf("blocked rejected snapshot = %#v", status)
	}
}

func TestRecordMarketPolicyJobKeepsPolicyAndAddsFeedback(t *testing.T) {
	app := testApp(t)
	app.configDir = t.TempDir()
	app.policy = map[string]MarketPolicyStatus{
		"auction": {
			Market:              "auction",
			Mode:                marketPolicyModeRecover,
			Reason:              "recovering",
			DBKinds:             10,
			EffectiveMaxActions: 100,
		},
	}
	app.recordMarketPolicyJob("auction", JobSummary{
		Status: MarketJobStatusPartialFailed,
		Error:  "1 actions failed",
		Plan:   &PlanSummary{Actions: 3},
		Actions: []ActionEntry{
			{OK: true},
			{OK: false, Action: Action{Market: marketNameAuction, ItemID: 1001}, Reason: bytePtr(152)},
			{OK: false, Action: Action{Market: marketNameAuction, ItemID: 1002}},
		},
	})

	status := app.policy["auction"]
	if status.Mode != marketPolicyModeRecover || status.Reason != "recovering" || status.DBKinds != 10 {
		t.Fatalf("policy judgement was overwritten: %#v", status)
	}
	if status.LastJobStatus != MarketJobStatusPartialFailed || status.LastJobError != "1 actions failed" || status.LastPlanActions != 3 || status.LastActionResults != 3 || status.LastActionFailed != 1 {
		t.Fatalf("job feedback not recorded: %#v", status)
	}
}

func TestRecordMarketPolicyJobIgnoresExplicitAuctionRejectsForServiceHealth(t *testing.T) {
	app := testApp(t)
	app.recordMarketPolicyJob("auction", JobSummary{
		Status: MarketJobStatusPartialFailed,
		Error:  "20 actions rejected",
		Actions: []ActionEntry{
			{OK: false, Action: Action{Market: marketNameAuction, ItemID: 1001}, Reason: bytePtr(152)},
			{OK: false, Action: Action{Market: marketNameAuction, ItemID: 1002}, Reason: bytePtr(152)},
			{OK: false, Action: Action{Market: marketNameAuction, ItemID: 1003}, Reason: bytePtr(152)},
		},
	})

	status := app.policy["auction"]
	if status.LastActionResults != 3 || status.LastActionFailed != 0 {
		t.Fatalf("explicit rejects should not count as service failures: %#v", status)
	}
}

func TestNextMarketPolicyStateIsPureCounterLogic(t *testing.T) {
	prev := MarketPolicyStatus{
		DBKinds:             10,
		StagnantRounds:      1,
		ZeroCandidateRounds: 1,
		UpdatedAt:           time.Now(),
	}
	state := nextMarketPolicyState("auction", 10, marketCandidateSnapshot{Count: 20}, prev)
	if state.mode != marketPolicyModeRecover || state.stagnantRounds != 2 || state.kindDelta != 0 {
		t.Fatalf("stagnant state = %#v", state)
	}

	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.stagnantRounds != 0 || state.kindDelta != 1 || state.mode != marketPolicyModeNormal {
		t.Fatalf("growth state = %#v", state)
	}

	state = nextMarketPolicyState("auction", 10, marketCandidateSnapshot{Count: 0}, prev)
	if state.zeroCandidateRounds != 2 || state.mode != marketPolicyModeRecover {
		t.Fatalf("zero candidate state = %#v", state)
	}

	prev = MarketPolicyStatus{DBKinds: 10, LastActionResults: 20, LastActionFailed: 10, UpdatedAt: time.Now()}
	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.actionFailureRounds != 1 || state.mode != marketPolicyModeRecover {
		t.Fatalf("high failure state = %#v", state)
	}

	prev = MarketPolicyStatus{DBKinds: 10, LastActionResults: 19, LastActionFailed: 19, UpdatedAt: time.Now()}
	state = nextMarketPolicyState("auction", 11, marketCandidateSnapshot{Count: 20}, prev)
	if state.actionFailureRounds != 0 {
		t.Fatalf("low sample failure state = %#v", state)
	}
}

func TestMarketPolicyDegradesWithoutRestartAfterRepeatedActionFailures(t *testing.T) {
	app := testApp(t)
	restarts := 0
	app.restarter = func(name, reason string) {
		restarts++
	}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	status := MarketPolicyStatus{Market: "auction", ActionFailureRounds: 2}
	policy := app.applyAuctionPolicyActions(marketCandidateSnapshot{Count: 100}, &status, marketAutoPolicy{MaxActions: 10000, MaxConcurrent: 8})

	if status.Mode != marketPolicyModeDegraded || policy.MaxActions != 2000 || policy.MaxConcurrent != 4 {
		t.Fatalf("repeated failure policy status=%#v policy=%#v", status, policy)
	}
	if restarts != 0 {
		t.Fatalf("restarts=%d, want 0", restarts)
	}
	if len(app.auctionQueue) != 1 || len(app.auctionRejected) != 1 {
		t.Fatalf("action failure backoff should keep queues: normal=%v rejected=%v", app.auctionQueue, app.auctionRejected)
	}
}

func TestMarketPolicyPrioritizesZeroKindRecoveryOverActionFailure(t *testing.T) {
	app := testApp(t)
	restarts := 0
	app.restarter = func(name, reason string) {
		if name != marketServiceNameAuction || !strings.Contains(reason, "zero") {
			t.Fatalf("unexpected restart name=%q reason=%q", name, reason)
		}
		restarts++
	}
	app.auctionQueue = []uint32{10075}
	app.auctionRejected = []uint32{30075}
	status := MarketPolicyStatus{Market: "auction", ZeroKindRounds: 2, ActionFailureRounds: 13}
	policy := app.applyAuctionPolicyActions(marketCandidateSnapshot{Count: 100}, &status, marketAutoPolicy{MaxActions: 10000, MaxConcurrent: 8})

	if restarts != 1 {
		t.Fatalf("restarts=%d, want 1", restarts)
	}
	if status.Mode != marketPolicyModeRecover || !strings.Contains(status.Reason, "zero") {
		t.Fatalf("zero-kind recovery was not prioritized: status=%#v", status)
	}
	if policy.MaxActions != 10000 || policy.MaxConcurrent != 8 {
		t.Fatalf("zero-kind recovery should not be overwritten by action-failure degradation: %#v", policy)
	}
	if len(app.auctionQueue) != 0 || len(app.auctionRejected) != 0 {
		t.Fatalf("queues were not reset after zero-kind recovery: normal=%v rejected=%v", app.auctionQueue, app.auctionRejected)
	}
}

func TestMarketServiceRestartCooldown(t *testing.T) {
	app := testApp(t)
	restarts := 0
	app.restarter = func(name, reason string) {
		restarts++
	}

	app.restartMarketService(marketServiceNameAuction, "first")
	app.restartMarketService(marketServiceNameAuction, "second")

	if restarts != 1 {
		t.Fatalf("restarts=%d, want 1", restarts)
	}
}

func TestMarketPolicyHealthCompletion(t *testing.T) {
	status := MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeNormal, DBKinds: 10, Candidates: 20, CandidateSource: "iteminfo.dat"}
	status.applyHealth()
	if status.Health != marketPolicyHealthHealthy || status.Completion != 100 {
		t.Fatalf("healthy status = %#v", status)
	}

	status = MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeRecover, DBKinds: 0, Candidates: 20, CandidateSource: "iteminfo.dat"}
	status.applyHealth()
	if status.Health != marketPolicyHealthBlocked || status.Completion != 40 {
		t.Fatalf("blocked status = %#v", status)
	}

	status = MarketPolicyStatus{Market: "auction", Mode: marketPolicyModeNormal, DBKinds: 10, Candidates: 20, CandidateSource: "iteminfo.dat", LastActionResults: 20, LastActionFailed: 10}
	status.applyHealth()
	if status.Health != marketPolicyHealthWarning || status.Completion != 70 {
		t.Fatalf("warning status = %#v", status)
	}
}
