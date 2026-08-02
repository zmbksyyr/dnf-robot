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
	return a.auctionUnitPriceFor(0, base, isEquipment, batchInflate, upgrade)
}

func (a *App) auctionUnitPriceFor(itemID uint32, base int32, isEquipment bool, batchInflate float64, upgrade int) int32 {
	if itemID > 0 {
		if priceRange, ok := a.customPriceRange(itemID); ok {
			return a.randomPriceInRange(priceRange.MinPrice, priceRange.MaxPrice)
		}
	}
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

func (a *App) randomPriceInRange(low, high int32) int32 {
	if low <= 0 {
		low = 1
	}
	if high < low {
		high = low
	}
	span := int64(high) - int64(low) + 1
	if span <= 1 {
		return low
	}
	return int32(int64(low) + a.rand.Int63n(span))
}

func (a *App) auctionPriceBounds(item catalogItem) (int32, int32) {
	if priceRange, ok := a.customPriceRange(item.ItemID); ok {
		return priceRange.MinPrice, priceRange.MaxPrice
	}
	base := float64(marketBasePrice(item))
	lowRand, highRand := a.cfg.Restock.RandLow, a.cfg.Restock.RandHigh
	if lowRand <= 0 {
		lowRand = 1
	}
	if highRand < lowRand {
		highRand = lowRand
	}
	low, high := base*lowRand, base*highRand
	if item.Kind == "equipment" {
		low = base * float64(a.cfg.Restock.EquipInflateMin) * lowRand
		high = base * float64(a.cfg.Restock.EquipInflateMax) * highRand
		if specialAuctionKind(item) == "" {
			low *= 1 + float64(a.cfg.Restock.UpgradeMin)*a.cfg.Restock.UpgradePriceRate
			high *= 1 + float64(a.cfg.Restock.UpgradeMax)*a.cfg.Restock.UpgradePriceRate
		}
	}
	return boundedAuctionPrice(low), boundedAuctionPrice(high)
}

func boundedAuctionPrice(price float64) int32 {
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
