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

	status := a.nextMarketPolicyStatus(market, kinds, policy)
	if market == "auction" {
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

func (a *App) nextMarketPolicyStatus(market string, kinds int, policy marketAutoPolicy) MarketPolicyStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.policy[market]
	zeroRounds := 0
	mode := marketPolicyModeNormal
	reason := ""
	if kinds <= 0 {
		zeroRounds = prev.ZeroKindRounds + 1
		reason = fmt.Sprintf("%s database has zero system item kinds", market)
		if zeroRounds > 1 {
			mode = marketPolicyModeRecover
		}
	}

	status := MarketPolicyStatus{
		Market:               market,
		Mode:                 mode,
		Reason:               reason,
		DBKinds:              kinds,
		ZeroKindRounds:       zeroRounds,
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

func (a *App) setMarketPolicyStatus(status MarketPolicyStatus) {
	a.mu.Lock()
	if a.policy == nil {
		a.policy = map[string]MarketPolicyStatus{}
	}
	prev, hadPrev := a.policy[status.Market]
	a.policy[status.Market] = status
	a.mu.Unlock()
	changed := !hadPrev || prev.Mode != status.Mode || prev.Reason != status.Reason || prev.EffectiveMaxActions != status.EffectiveMaxActions || prev.EffectiveConcurrency != status.EffectiveConcurrency
	if status.Mode != marketPolicyModeNormal || changed && hadPrev {
		a.appendLog(LogEvent{Type: "market_policy", Market: status.Market, Status: status.Mode, Message: status.Reason})
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
