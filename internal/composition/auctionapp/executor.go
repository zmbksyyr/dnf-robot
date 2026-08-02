package auctionapp

import (
	"context"
	"fmt"

	"robot/internal/capability/marketapp"
	"robot/internal/protocol/auction"
)

type Factory struct{}

func NewFactory() Factory {
	return Factory{}
}

func (Factory) NewActionExecutor(cfg marketapp.Config) marketapp.ActionExecutor {
	return &actionExecutor{
		cfg: cfg,
		rs:  auction.NewRobotSvc(),
	}
}

type actionExecutor struct {
	cfg            marketapp.Config
	rs             *auction.RobotSvc
	auctionSession *auction.Session
	ceraSession    *auction.Session
}

func (e *actionExecutor) Close() {
	if e.auctionSession != nil {
		_ = e.auctionSession.Close()
	}
	if e.ceraSession != nil {
		_ = e.ceraSession.Close()
	}
}

func (e *actionExecutor) Execute(ctx context.Context, action marketapp.Action) (marketapp.ActionExecutionResult, error) {
	res, err := e.executeDirect(ctx, action)
	if err != nil {
		return marketapp.ActionExecutionResult{}, err
	}
	return marketapp.ActionExecutionResult{
		ResultOK:     res.ResultOK,
		ResultReason: res.ResultReason,
		AuctionID:    res.AuctionID,
		Raw:          res,
	}, nil
}

func (e *actionExecutor) executeDirect(ctx context.Context, action marketapp.Action) (auction.Result, error) {
	res, err := e.executeDirectWithSession(ctx, action)
	if err == nil {
		return res, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return auction.Result{}, ctx.Err()
	}
	e.resetSession(action.Market)
	return e.executeDirectWithSession(ctx, action)
}

func (e *actionExecutor) executeDirectWithSession(ctx context.Context, action marketapp.Action) (auction.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return auction.Result{}, err
	}
	if action.Operation == "collect" {
		return e.withSession(ctx, action.Market, func(session *auction.Session) (auction.Result, error) {
			return session.Bid(auction.BidRequest{
				Host:      e.cfg.AuctionHost,
				Port:      e.cfg.AuctionPort,
				Point:     action.Market == "cera",
				CID:       action.OwnerID,
				BuyerID:   action.OwnerID,
				BuyerName: action.OwnerName,
				Money:     action.InstantPrice,
				AuctionID: action.AuctionID,
				TimeoutMS: 5000,
			})
		})
	}
	switch action.Market {
	case "auction":
		return e.withSession(ctx, action.Market, func(session *auction.Session) (auction.Result, error) {
			return session.RegisterItem(auction.RegisterItemRequest{
				Host:           e.cfg.AuctionHost,
				Port:           e.cfg.AuctionPort,
				CID:            action.OwnerID,
				OwnerID:        action.OwnerID,
				OwnerName:      action.OwnerName,
				OwnerType:      1,
				ItemID:         action.ItemID,
				CountOrAddInfo: action.CountAddInfo,
				ItemType:       byte(action.ItemType),
				ItemAttr:       byte(action.Upgrade),
				Endurance:      uint16(action.Endurance),
				HasEndurance:   action.HasEndurance,
				ExtraAddInfo:   action.ExtraAddInfo,
				StartPrice:     action.StartPrice,
				InstantPrice:   action.InstantPrice,
				UnitPrice:      action.UnitPrice,
				TimeoutMS:      5000,
			})
		})
	case "cera":
		return e.withSession(ctx, action.Market, func(session *auction.Session) (auction.Result, error) {
			return session.RegisterGold(auction.RegisterGoldRequest{
				Host:       e.cfg.CeraHost,
				Port:       e.cfg.CeraPort,
				CID:        action.OwnerID,
				OwnerID:    action.OwnerID,
				OwnerName:  action.OwnerName,
				OwnerType:  1,
				ItemID:     action.ItemID,
				GoldAmount: action.CountAddInfo,
				CeraPrice:  action.InstantPrice,
				TimeoutMS:  5000,
			})
		})
	default:
		return auction.Result{}, fmt.Errorf("unsupported market %q", action.Market)
	}
}

func (e *actionExecutor) withSession(ctx context.Context, market string, call func(*auction.Session) (auction.Result, error)) (auction.Result, error) {
	session, err := e.session(ctx, market)
	if err != nil {
		return auction.Result{}, err
	}
	stopCancel := closeSessionOnCancel(ctx, session)
	result, err := call(session)
	stopCancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		e.resetSession(market)
		return auction.Result{}, ctxErr
	}
	return result, err
}

func closeSessionOnCancel(ctx context.Context, session *auction.Session) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = session.Close()
		close(done)
	})
	return func() {
		if !stop() {
			<-done
		}
	}
}

func (e *actionExecutor) session(ctx context.Context, market string) (*auction.Session, error) {
	switch market {
	case "cera":
		if e.ceraSession == nil {
			s, err := auction.NewSessionContext(ctx, e.cfg.CeraHost, e.cfg.CeraPort, 5000, true, true)
			if err != nil {
				return nil, err
			}
			e.ceraSession = s
		}
		return e.ceraSession, nil
	default:
		if e.auctionSession == nil {
			s, err := auction.NewSessionContext(ctx, e.cfg.AuctionHost, e.cfg.AuctionPort, 5000, false, false)
			if err != nil {
				return nil, err
			}
			e.auctionSession = s
		}
		return e.auctionSession, nil
	}
}

func (e *actionExecutor) resetSession(market string) {
	switch market {
	case "cera":
		if e.ceraSession != nil {
			_ = e.ceraSession.Close()
			e.ceraSession = nil
		}
	default:
		if e.auctionSession != nil {
			_ = e.auctionSession.Close()
			e.auctionSession = nil
		}
	}
}
