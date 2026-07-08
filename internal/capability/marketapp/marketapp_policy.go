package marketapp

import (
	"fmt"
	"strings"
	"time"
)

const (
	marketPolicyModeNormal   = "normal"
	marketPolicyModeRecover  = "recover"
	marketPolicyModeDegraded = "degraded"
)

const (
	marketPolicyHealthHealthy    = "healthy"
	marketPolicyHealthRecovering = "recovering"
	marketPolicyHealthDegraded   = "degraded"
	marketPolicyHealthBlocked    = "blocked"
	marketPolicyHealthWarning    = "warning"
)

type marketAutoPolicy struct {
	MaxActions    int
	MaxConcurrent int
}

type marketCandidateSnapshot struct {
	Count   int
	Special int
	Source  string
	Error   string
}

func (a *App) marketAutoPolicy(market string, cfg AutoCfg) marketAutoPolicy {
	market = normalizeMarketName(market)
	policy := marketAutoPolicy{MaxActions: cfg.MaxActions, MaxConcurrent: cfg.MaxConcurrent}
	kinds, err := a.currentMarketKinds(market)
	if err != nil {
		a.setMarketPolicyStatus(MarketPolicyStatus{
			Market:               market,
			Mode:                 marketPolicyModeDegraded,
			Reason:               err.Error(),
			EffectiveMaxActions:  policy.MaxActions,
			EffectiveConcurrency: policy.MaxConcurrent,
			UpdatedAt:            time.Now(),
		})
		return policy
	}

	candidates := marketCandidateSnapshot{}
	if market == marketNameAuction {
		candidates = a.observeAuctionCandidates()
	}
	status := a.nextMarketPolicyStatus(market, kinds, candidates, policy)
	if market == marketNameAuction {
		policy = a.applyAuctionPolicyActions(candidates, &status, policy)
	}
	a.setMarketPolicyStatus(status)
	return policy
}

func (a *App) applyAuctionPolicyActions(candidates marketCandidateSnapshot, status *MarketPolicyStatus, policy marketAutoPolicy) marketAutoPolicy {
	if candidates.Error != "" {
		return degradeMarketPolicy(status, policy, candidates.Error)
	}
	switch {
	case status.ZeroCandidateRounds == 2:
		a.recoverAuctionQueues("zero_candidate_recovery")
		status.Reason = "auction candidates stayed zero; auction queues rebuilt"
		status.Mode = marketPolicyModeRecover
	case status.ZeroCandidateRounds >= 3:
		return degradeMarketPolicy(status, policy, "auction candidates still zero; send pressure reduced")
	}
	switch {
	case status.StagnantRounds == 2:
		a.recoverAuctionQueues("stagnant_growth_recovery")
		status.Reason = "auction kinds stopped growing below candidate count; auction queues rebuilt"
		status.Mode = marketPolicyModeRecover
	case status.StagnantRounds >= 3:
		return degradeMarketPolicy(status, policy, "auction kinds still not growing below candidate count; send pressure reduced")
	}
	switch {
	case status.ZeroKindRounds == 2:
		a.recoverAuctionService("zero_kind_recovery", "auction kinds stayed zero")
		status.Reason = "auction kinds stayed zero; auction service restarted and queues rebuilt"
		status.Mode = marketPolicyModeRecover
		return policy
	case status.ZeroKindRounds >= 3:
		return degradeMarketPolicy(status, policy, "auction kinds still zero; send pressure reduced")
	}
	switch {
	case status.ActionFailureRounds == 1:
		status.Reason = "auction action failures are high; observing one recovery round"
		status.Mode = marketPolicyModeRecover
	case status.ActionFailureRounds == 2:
		a.recoverAuctionService("action_failure_recovery", "auction action failures stayed high")
		return degradeMarketPolicy(status, policy, "auction action failures stayed high; auction service restarted and send pressure reduced")
	case status.ActionFailureRounds >= 2:
		return degradeMarketPolicy(status, policy, "auction action failures stayed high; send pressure reduced")
	}
	return policy
}

func (a *App) recoverAuctionQueues(message string) {
	a.resetAuctionQueues()
	a.appendLog(LogEvent{Type: "market_policy", Market: marketNameAuction, Status: marketLogStatusQueueReset, Message: message})
}

func (a *App) recoverAuctionService(message, restartReason string) {
	a.recoverAuctionQueues(message)
	a.restartMarketService(marketServiceNameAuction, restartReason)
}

func degradeMarketPolicy(status *MarketPolicyStatus, policy marketAutoPolicy, reason string) marketAutoPolicy {
	status.Mode = marketPolicyModeDegraded
	status.Reason = reason
	policy.MaxActions = minPositive(policy.MaxActions, 2000)
	policy.MaxConcurrent = minPositive(policy.MaxConcurrent, 4)
	status.EffectiveMaxActions = policy.MaxActions
	status.EffectiveConcurrency = policy.MaxConcurrent
	return policy
}

func (a *App) nextMarketPolicyStatus(market string, kinds int, candidates marketCandidateSnapshot, policy marketAutoPolicy) MarketPolicyStatus {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	prev := a.policy[market]
	state := nextMarketPolicyState(market, kinds, candidates, prev)

	status := MarketPolicyStatus{
		Market:               market,
		Mode:                 state.mode,
		Reason:               state.reason,
		DBKinds:              kinds,
		KindDelta:            state.kindDelta,
		Candidates:           candidates.Count,
		SpecialCandidates:    candidates.Special,
		CandidateSource:      candidates.Source,
		ZeroKindRounds:       state.zeroKindRounds,
		ZeroCandidateRounds:  state.zeroCandidateRounds,
		StagnantRounds:       state.stagnantRounds,
		ActionFailureRounds:  state.actionFailureRounds,
		EffectiveMaxActions:  policy.MaxActions,
		EffectiveConcurrency: policy.MaxConcurrent,
		UpdatedAt:            time.Now(),
	}
	if market == marketNameAuction {
		status.QueueNormal = len(a.auctionQueue)
		status.QueueSpecial = len(a.auctionSpecialQueue)
		status.QueueRejected = len(a.auctionRejected)
		status.QueueSource = a.auctionQueueSource
	}
	status.applyHealth()
	return status
}

type marketPolicyState struct {
	mode                string
	reason              string
	kindDelta           int
	zeroKindRounds      int
	zeroCandidateRounds int
	stagnantRounds      int
	actionFailureRounds int
}

func nextMarketPolicyState(market string, kinds int, candidates marketCandidateSnapshot, prev MarketPolicyStatus) marketPolicyState {
	state := marketPolicyState{mode: marketPolicyModeNormal}
	if !prev.UpdatedAt.IsZero() {
		state.kindDelta = kinds - prev.DBKinds
	}
	if kinds <= 0 {
		state.zeroKindRounds = prev.ZeroKindRounds + 1
		state.reason = fmt.Sprintf("%s database has zero system item kinds", market)
		if state.zeroKindRounds > 1 {
			state.mode = marketPolicyModeRecover
		}
	}
	if market == marketNameAuction && candidates.Error == "" && candidates.Count <= 0 {
		state.zeroCandidateRounds = prev.ZeroCandidateRounds + 1
		if state.reason == "" {
			state.reason = "auction has zero candidate item kinds"
		}
		if state.zeroCandidateRounds > 1 {
			state.mode = marketPolicyModeRecover
		}
	}
	if market == marketNameAuction && candidates.Error == "" && candidates.Count > kinds && kinds > 0 && !prev.UpdatedAt.IsZero() && kinds <= prev.DBKinds {
		state.stagnantRounds = prev.StagnantRounds + 1
		if state.reason == "" {
			state.reason = "auction item kinds are not growing below candidate count"
		}
		if state.stagnantRounds > 1 {
			state.mode = marketPolicyModeRecover
		}
	}
	if highMarketActionFailure(prev) {
		state.actionFailureRounds = prev.ActionFailureRounds + 1
		if state.reason == "" {
			state.reason = "auction action failures are high"
		}
		if state.actionFailureRounds > 0 {
			state.mode = marketPolicyModeRecover
		}
	}
	return state
}

func highMarketActionFailure(status MarketPolicyStatus) bool {
	if status.LastActionResults < 20 {
		return false
	}
	return status.LastActionFailed*2 >= status.LastActionResults
}

func (a *App) observeAuctionCandidates() marketCandidateSnapshot {
	catalog, err := a.loadCatalog()
	if err != nil {
		rows, fallbackErr := a.fallbackAuctionRows()
		if fallbackErr != nil {
			return marketCandidateSnapshot{Source: marketCandidateSourceUnavailable, Error: fmt.Sprintf("auction catalog unavailable: %v; fallback unavailable: %v", err, fallbackErr)}
		}
		return marketCandidateSnapshot{Count: countEnabledAuctionRows(rows), Source: marketQueueSourceFallback}
	}
	itemInfoIDs, path, err := a.currentItemInfoIDs()
	if err != nil {
		return marketCandidateSnapshot{Source: marketQueueSourcePVFItemInfoMissing, Error: err.Error()}
	}
	normal, special := catalogAuctionCandidateCounts(catalog, itemInfoIDs)
	return marketCandidateSnapshot{Count: normal, Special: special, Source: path}
}

func (a *App) setMarketPolicyStatus(status MarketPolicyStatus) {
	status.applyHealth()
	a.stateMu.Lock()
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	prev, hadPrev := a.policy[status.Market]
	a.policy[status.Market] = status
	a.stateMu.Unlock()
	changed := !hadPrev || prev.Mode != status.Mode || prev.Reason != status.Reason || prev.EffectiveMaxActions != status.EffectiveMaxActions || prev.EffectiveConcurrency != status.EffectiveConcurrency
	if changed && (status.Mode != marketPolicyModeNormal || hadPrev) {
		a.appendLog(LogEvent{Type: "market_policy", Market: status.Market, Status: status.Mode, Message: status.Reason})
	}
}

func (a *App) markMarketPolicyBlocked(market, reason string) {
	market = normalizeMarketName(market)
	a.stateMu.Lock()
	status := MarketPolicyStatus{
		Market:      market,
		Mode:        marketPolicyModeDegraded,
		Reason:      reason,
		UpdatedAt:   time.Now(),
		QueueSource: "",
	}
	if market == marketNameAuction {
		status.QueueNormal = len(a.auctionQueue)
		status.QueueSpecial = len(a.auctionSpecialQueue)
		status.QueueRejected = len(a.auctionRejected)
		status.QueueSource = a.auctionQueueSource
	}
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	status.applyHealth()
	prev, hadPrev := a.policy[market]
	a.policy[market] = status
	a.stateMu.Unlock()
	if !hadPrev || prev.Mode != status.Mode || prev.Reason != status.Reason {
		a.appendLog(LogEvent{Type: "market_policy", Market: market, Status: status.Mode, Message: reason})
	}
}

func (a *App) recordMarketPolicyJob(market string, job JobSummary) {
	market = normalizeMarketName(market)
	a.stateMu.Lock()
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	status := a.policy[market]
	status.Market = market
	status.LastJobStatus = job.Status
	status.LastJobError = job.Error
	if job.Plan != nil {
		status.LastPlanActions = job.Plan.Actions
	}
	status.LastActionResults = len(job.Actions)
	status.LastActionFailed = countFailedActionEntries(job.Actions)
	status.UpdatedAt = time.Now()
	status.applyHealth()
	a.policy[market] = status
	a.stateMu.Unlock()
}

func (a *App) currentMarketKinds(market string) (int, error) {
	occ := map[uint32]int{}
	switch normalizeMarketName(market) {
	case marketNameAuction:
		have, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, occ)
		return len(have), err
	case marketNameCera:
		have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, occ)
		return len(have), err
	default:
		return 0, nil
	}
}

func countFailedActionEntries(entries []ActionEntry) int {
	failed := 0
	for _, entry := range entries {
		if !entry.OK || entry.Error != "" {
			failed++
		}
	}
	return failed
}

func (s *MarketPolicyStatus) applyHealth() {
	switch s.Mode {
	case marketPolicyModeDegraded:
		s.Health = marketPolicyHealthDegraded
		s.Completion = 50
	case marketPolicyModeRecover:
		s.Health = marketPolicyHealthRecovering
		s.Completion = 80
	default:
		s.Health = marketPolicyHealthHealthy
		s.Completion = 100
	}
	if s.Market == marketNameAuction {
		if s.CandidateSource != "" && s.Candidates <= 0 {
			s.Health = marketPolicyHealthBlocked
			s.Completion = minPositive(s.Completion, 40)
		}
		if s.DBKinds <= 0 && s.Mode != marketPolicyModeNormal {
			s.Health = marketPolicyHealthBlocked
			s.Completion = minPositive(s.Completion, 40)
		}
	}
	if s.LastActionResults >= 20 && s.LastActionFailed*2 >= s.LastActionResults {
		s.Completion = minPositive(s.Completion, 70)
		if s.Health == marketPolicyHealthHealthy {
			s.Health = marketPolicyHealthWarning
		}
	}
	if s.Completion < 0 {
		s.Completion = 0
	}
	if s.Completion > 100 {
		s.Completion = 100
	}
}

func normalizeMarketName(market string) string {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case marketNameCera, marketAliasGold, marketAliasPoint:
		return marketNameCera
	case "", marketNameAuction:
		return marketNameAuction
	default:
		return strings.ToLower(strings.TrimSpace(market))
	}
}

func minPositive(v, limit int) int {
	if v <= 0 {
		return limit
	}
	if v < limit {
		return v
	}
	return limit
}

func countEnabledAuctionRows(rows []restockRow) int {
	count := 0
	for _, row := range rows {
		if row.ItemID != 0 && row.Enabled {
			count++
		}
	}
	return count
}
