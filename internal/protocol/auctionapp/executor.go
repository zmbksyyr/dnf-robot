package auctionapp

import (
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

func (e *actionExecutor) Execute(action marketapp.Action) (marketapp.ActionExecutionResult, error) {
	res, err := e.executeDirect(action)
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

func (e *actionExecutor) executeDirect(action marketapp.Action) (auction.Result, error) {
	res, err := e.executeDirectWithSession(action)
	if err == nil {
		return res, nil
	}
	e.resetSession(action.Market)
	return e.executeDirectWithSession(action)
}

func (e *actionExecutor) executeDirectWithSession(action marketapp.Action) (auction.Result, error) {
	if action.Operation == "collect" {
		session, err := e.session(action.Market)
		if err != nil {
			return auction.Result{}, err
		}
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
	}
	switch action.Market {
	case "auction":
		session, err := e.session(action.Market)
		if err != nil {
			return auction.Result{}, err
		}
		return session.RegisterItem(auction.RegisterItemRequest{
			Host:           e.cfg.AuctionHost,
			Port:           e.cfg.AuctionPort,
			CID:            action.OwnerID,
			OwnerID:        action.OwnerID,
			OwnerName:      action.OwnerName,
			ItemID:         action.ItemID,
			CountOrAddInfo: action.CountAddInfo,
			ItemAttr:       byte(action.Upgrade),
			ExtraAddInfo:   action.ExtraAddInfo,
			StartPrice:     action.StartPrice,
			InstantPrice:   action.InstantPrice,
			TimeoutMS:      5000,
		})
	case "cera":
		session, err := e.session(action.Market)
		if err != nil {
			return auction.Result{}, err
		}
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
	default:
		return auction.Result{}, fmt.Errorf("unsupported market %q", action.Market)
	}
}

func (e *actionExecutor) session(market string) (*auction.Session, error) {
	switch market {
	case "cera":
		if e.ceraSession == nil {
			s, err := auction.NewSession(e.cfg.CeraHost, e.cfg.CeraPort, 5000, true, true)
			if err != nil {
				return nil, err
			}
			e.ceraSession = s
		}
		return e.ceraSession, nil
	default:
		if e.auctionSession == nil {
			s, err := auction.NewSession(e.cfg.AuctionHost, e.cfg.AuctionPort, 5000, false, false)
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
