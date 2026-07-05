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

type marketAutoPolicy struct {
	MaxActions    int
	MaxConcurrent int
}

type marketCandidateSnapshot struct {
	Count  int
	Source string
	Error  string
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
	if market == "auction" {
		candidates = a.observeAuctionCandidates()
	}
	status := a.nextMarketPolicyStatus(market, kinds, candidates, policy)
	if market == "auction" {
		if candidates.Error != "" {
			status.Mode = marketPolicyModeDegraded
			status.Reason = candidates.Error
			policy.MaxActions = minPositive(policy.MaxActions, 2000)
			policy.MaxConcurrent = minPositive(policy.MaxConcurrent, 4)
			status.EffectiveMaxActions = policy.MaxActions
			status.EffectiveConcurrency = policy.MaxConcurrent
		}
		if candidates.Error == "" && status.ZeroCandidateRounds == 2 {
			a.resetAuctionQueues()
			a.appendLog(LogEvent{Type: "market_policy", Market: "auction", Status: "queue_reset", Message: "zero_candidate_recovery"})
			status.Reason = "auction candidates stayed zero; auction queues rebuilt"
			status.Mode = marketPolicyModeRecover
		}
		if candidates.Error == "" && status.ZeroCandidateRounds >= 3 {
			status.Mode = marketPolicyModeDegraded
			status.Reason = "auction candidates still zero; send pressure reduced"
			policy.MaxActions = minPositive(policy.MaxActions, 2000)
			policy.MaxConcurrent = minPositive(policy.MaxConcurrent, 4)
			status.EffectiveMaxActions = policy.MaxActions
			status.EffectiveConcurrency = policy.MaxConcurrent
		}
		if candidates.Error == "" && status.StagnantRounds == 2 {
			a.resetAuctionQueues()
			a.appendLog(LogEvent{Type: "market_policy", Market: "auction", Status: "queue_reset", Message: "stagnant_growth_recovery"})
			status.Reason = "auction kinds stopped growing below candidate count; auction queues rebuilt"
			status.Mode = marketPolicyModeRecover
		}
		if candidates.Error == "" && status.StagnantRounds >= 3 {
			status.Mode = marketPolicyModeDegraded
			status.Reason = "auction kinds still not growing below candidate count; send pressure reduced"
			policy.MaxActions = minPositive(policy.MaxActions, 2000)
			policy.MaxConcurrent = minPositive(policy.MaxConcurrent, 4)
			status.EffectiveMaxActions = policy.MaxActions
			status.EffectiveConcurrency = policy.MaxConcurrent
		}
		if status.ZeroKindRounds == 2 {
			a.resetAuctionQueues()
			a.appendLog(LogEvent{Type: "market_policy", Market: "auction", Status: "queue_reset", Message: "zero_kind_recovery"})
			status.Reason = "auction kinds stayed zero; auction queues rebuilt"
			status.Mode = marketPolicyModeRecover
		}
		if status.ZeroKindRounds >= 3 {
			status.Mode = marketPolicyModeDegraded
			status.Reason = "auction kinds still zero; send pressure reduced"
			policy.MaxActions = minPositive(policy.MaxActions, 2000)
			policy.MaxConcurrent = minPositive(policy.MaxConcurrent, 4)
			status.EffectiveMaxActions = policy.MaxActions
			status.EffectiveConcurrency = policy.MaxConcurrent
		}
	}
	a.setMarketPolicyStatus(status)
	return policy
}

func (a *App) nextMarketPolicyStatus(market string, kinds int, candidates marketCandidateSnapshot, policy marketAutoPolicy) MarketPolicyStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.policy[market]
	zeroRounds := 0
	zeroCandidates := 0
	stagnantRounds := 0
	kindDelta := 0
	mode := marketPolicyModeNormal
	reason := ""
	if !prev.UpdatedAt.IsZero() {
		kindDelta = kinds - prev.DBKinds
	}
	if kinds <= 0 {
		zeroRounds = prev.ZeroKindRounds + 1
		reason = fmt.Sprintf("%s database has zero system item kinds", market)
		if zeroRounds > 1 {
			mode = marketPolicyModeRecover
		}
	}
	if market == "auction" && candidates.Error == "" && candidates.Count <= 0 {
		zeroCandidates = prev.ZeroCandidateRounds + 1
		if reason == "" {
			reason = "auction has zero candidate item kinds"
		}
		if zeroCandidates > 1 {
			mode = marketPolicyModeRecover
		}
	}
	if market == "auction" && candidates.Error == "" && candidates.Count > kinds && kinds > 0 && !prev.UpdatedAt.IsZero() {
		if kinds <= prev.DBKinds {
			stagnantRounds = prev.StagnantRounds + 1
			if reason == "" {
				reason = "auction item kinds are not growing below candidate count"
			}
			if stagnantRounds > 1 {
				mode = marketPolicyModeRecover
			}
		}
	}

	status := MarketPolicyStatus{
		Market:               market,
		Mode:                 mode,
		Reason:               reason,
		DBKinds:              kinds,
		KindDelta:            kindDelta,
		Candidates:           candidates.Count,
		CandidateSource:      candidates.Source,
		ZeroKindRounds:       zeroRounds,
		ZeroCandidateRounds:  zeroCandidates,
		StagnantRounds:       stagnantRounds,
		EffectiveMaxActions:  policy.MaxActions,
		EffectiveConcurrency: policy.MaxConcurrent,
		UpdatedAt:            time.Now(),
	}
	if market == "auction" {
		status.QueueNormal = len(a.auctionQueue)
		status.QueueRejected = len(a.auctionRejected)
		status.QueueSource = a.auctionQueueSource
	}
	return status
}

func (a *App) observeAuctionCandidates() marketCandidateSnapshot {
	catalog, err := a.loadCatalog()
	if err != nil {
		rows, fallbackErr := a.fallbackAuctionRows()
		if fallbackErr != nil {
			return marketCandidateSnapshot{Source: "unavailable", Error: fmt.Sprintf("auction catalog unavailable: %v; fallback unavailable: %v", err, fallbackErr)}
		}
		return marketCandidateSnapshot{Count: countEnabledAuctionRows(rows), Source: "fallback"}
	}
	itemInfoIDs, path, err := a.currentItemInfoIDs()
	if err != nil {
		return marketCandidateSnapshot{Source: "pvf_iteminfo_missing", Error: err.Error()}
	}
	return marketCandidateSnapshot{Count: len(catalogAuctionIDs(catalog, itemInfoIDs)), Source: path}
}

func (a *App) setMarketPolicyStatus(status MarketPolicyStatus) {
	a.mu.Lock()
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	prev, hadPrev := a.policy[status.Market]
	a.policy[status.Market] = status
	a.mu.Unlock()
	changed := !hadPrev || prev.Mode != status.Mode || prev.Reason != status.Reason || prev.EffectiveMaxActions != status.EffectiveMaxActions || prev.EffectiveConcurrency != status.EffectiveConcurrency
	if changed && (status.Mode != marketPolicyModeNormal || hadPrev) {
		a.appendLog(LogEvent{Type: "market_policy", Market: status.Market, Status: status.Mode, Message: status.Reason})
	}
}

func (a *App) markMarketPolicyBlocked(market, reason string) {
	market = normalizeMarketName(market)
	a.mu.Lock()
	status := MarketPolicyStatus{
		Market:      market,
		Mode:        marketPolicyModeDegraded,
		Reason:      reason,
		UpdatedAt:   time.Now(),
		QueueSource: "",
	}
	if market == "auction" {
		status.QueueNormal = len(a.auctionQueue)
		status.QueueRejected = len(a.auctionRejected)
		status.QueueSource = a.auctionQueueSource
	}
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	prev, hadPrev := a.policy[market]
	a.policy[market] = status
	a.mu.Unlock()
	if !hadPrev || prev.Mode != status.Mode || prev.Reason != status.Reason {
		a.appendLog(LogEvent{Type: "market_policy", Market: market, Status: status.Mode, Message: reason})
	}
}

func (a *App) currentMarketKinds(market string) (int, error) {
	occ := map[uint32]int{}
	switch normalizeMarketName(market) {
	case "auction":
		have, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, occ)
		return len(have), err
	case "cera":
		have, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, occ)
		return len(have), err
	default:
		return 0, nil
	}
}

func normalizeMarketName(market string) string {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case "cera", "gold", "point":
		return "cera"
	case "", "auction":
		return "auction"
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
