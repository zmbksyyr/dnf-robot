package marketapp

func (a *App) price(base int32) int32 {
	if base <= 0 {
		base = 1
	}
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low <= 0 || high <= 0 || low == high {
		return base
	}
	v := float64(base) * (low + a.rand.Float64()*(high-low))
	if v < 1 {
		return 1
	}
	if v > float64(maxInt32) {
		return maxInt32
	}
	return int32(v)
}

func (a *App) auctionUnitPrice(base int32, isEquipment bool, batchInflate float64, upgrade int) int32 {
	if !isEquipment {
		return a.price(base)
	}
	if base <= 0 {
		base = 1000
	}
	if batchInflate <= 0 {
		batchInflate = 1
	}
	price := float64(base) * batchInflate
	price *= 1 + float64(upgrade)*a.cfg.Restock.UpgradePriceRate
	low, high := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if low > 0 && high > 0 && low != high {
		if high < low {
			high = low
		}
		price *= low + a.rand.Float64()*(high-low)
	}
	if price < 1 {
		return 1
	}
	const maxAuctionPrice = int32(2_000_000_000)
	if price > float64(maxAuctionPrice) {
		return maxAuctionPrice
	}
	return int32(price)
}

func marketBasePrice(item catalogItem) int32 {
	base := item.Price
	if base <= 0 {
		base = item.Value
	}
	if base <= 0 {
		base = 1000
	}
	return base
}

func (a *App) pickOwner(occ map[uint32]int) uint32 {
	owner := a.cfg.SystemOwner.IDBase
	for occ[owner] >= a.cfg.SystemOwner.RotateEvery {
		owner++
	}
	occ[owner]++
	return owner
}

func (a *App) nextSpecialAddInfo() int32 {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.specialAddInfo < specialAddInfoBase {
		a.specialAddInfo = specialAddInfoBase
		if a.repository != nil {
			if max, err := a.repository.LoadMaxAddInfo(a.cfg.AuctionDB, specialAddInfoBase); err == nil && max >= a.specialAddInfo && max < maxInt32 {
				a.specialAddInfo = max + 1
			}
		}
	}
	if a.specialAddInfo <= 0 || a.specialAddInfo >= maxInt32 {
		a.specialAddInfo = specialAddInfoBase
	}
	v := a.specialAddInfo
	a.specialAddInfo++
	return v
}
