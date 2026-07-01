package marketapp

import (
	"fmt"

	"robot/internal/service"
)

type actionExecutor struct {
	app            *App
	auctionSession *service.MarketDirectAuctionSession
	ceraSession    *service.MarketDirectAuctionSession
}

func (a *App) newActionExecutor() *actionExecutor {
	return &actionExecutor{app: a}
}

func (e *actionExecutor) close() {
	if e.auctionSession != nil {
		_ = e.auctionSession.Close()
	}
	if e.ceraSession != nil {
		_ = e.ceraSession.Close()
	}
}

func (e *actionExecutor) execute(action Action) (service.MarketDirectAuctionResult, error) {
	if action.Operation == "collect" {
		return e.collect(action)
	}
	switch action.Market {
	case "auction":
		if e.auctionSession == nil {
			s, err := service.NewMarketDirectAuctionSession(e.app.cfg.AuctionHost, e.app.cfg.AuctionPort, 5000, false, false)
			if err != nil {
				return service.MarketDirectAuctionResult{}, err
			}
			e.auctionSession = s
		}
		return e.auctionSession.RegisterItem(service.MarketDirectRegisterItemRequest{
			CID:            action.OwnerID,
			OwnerID:        action.OwnerID,
			OwnerName:      action.OwnerName,
			ItemID:         action.ItemID,
			CountOrAddInfo: action.CountAddInfo,
			StartPrice:     action.StartPrice,
			InstantPrice:   action.InstantPrice,
		})
	case "cera":
		if e.ceraSession == nil {
			s, err := service.NewMarketDirectAuctionSession(e.app.cfg.CeraHost, e.app.cfg.CeraPort, 5000, true, false)
			if err != nil {
				return service.MarketDirectAuctionResult{}, err
			}
			e.ceraSession = s
		}
		return e.ceraSession.RegisterGold(service.MarketDirectRegisterGoldRequest{
			CID:        action.OwnerID,
			OwnerID:    action.OwnerID,
			OwnerName:  action.OwnerName,
			OwnerType:  1,
			ItemID:     action.ItemID,
			GoldAmount: action.CountAddInfo,
			CeraPrice:  action.InstantPrice,
		})
	default:
		return service.MarketDirectAuctionResult{}, fmt.Errorf("unsupported market %q", action.Market)
	}
}

func (e *actionExecutor) collect(action Action) (service.MarketDirectAuctionResult, error) {
	point := action.Market == "cera"
	if point {
		if e.ceraSession == nil {
			s, err := service.NewMarketDirectAuctionSession(e.app.cfg.CeraHost, e.app.cfg.CeraPort, 5000, true, false)
			if err != nil {
				return service.MarketDirectAuctionResult{}, err
			}
			e.ceraSession = s
		}
		return e.ceraSession.Bid(service.MarketDirectBidRequest{
			Point:     true,
			CID:       action.OwnerID,
			BuyerID:   action.OwnerID,
			BuyerName: action.OwnerName,
			Money:     action.InstantPrice,
			AuctionID: action.AuctionID,
		})
	}
	if e.auctionSession == nil {
		s, err := service.NewMarketDirectAuctionSession(e.app.cfg.AuctionHost, e.app.cfg.AuctionPort, 5000, false, false)
		if err != nil {
			return service.MarketDirectAuctionResult{}, err
		}
		e.auctionSession = s
	}
	return e.auctionSession.Bid(service.MarketDirectBidRequest{
		CID:       action.OwnerID,
		BuyerID:   action.OwnerID,
		BuyerName: action.OwnerName,
		Money:     action.InstantPrice,
		AuctionID: action.AuctionID,
	})
}

func (a *App) executeAction(action Action) (service.MarketDirectAuctionResult, error) {
	if action.Operation == "collect" {
		return a.rs.MarketDirectBid(service.MarketDirectBidRequest{
			Host:      a.cfg.AuctionHost,
			Port:      a.cfg.AuctionPort,
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
		return a.rs.MarketDirectRegisterItem(service.MarketDirectRegisterItemRequest{
			Host:           a.cfg.AuctionHost,
			Port:           a.cfg.AuctionPort,
			CID:            action.OwnerID,
			OwnerID:        action.OwnerID,
			OwnerName:      action.OwnerName,
			ItemID:         action.ItemID,
			CountOrAddInfo: action.CountAddInfo,
			StartPrice:     action.StartPrice,
			InstantPrice:   action.InstantPrice,
			TimeoutMS:      5000,
		})
	case "cera":
		return a.rs.MarketDirectRegisterGold(service.MarketDirectRegisterGoldRequest{
			Host:       a.cfg.CeraHost,
			Port:       a.cfg.CeraPort,
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
		return service.MarketDirectAuctionResult{}, fmt.Errorf("unsupported market %q", action.Market)
	}
}
