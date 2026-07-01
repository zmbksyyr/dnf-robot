package marketapp

import "strings"

func (a *App) loadSystemStock() (map[uint32]int, map[uint32]int, map[uint32]int, error) {
	occ := map[uint32]int{}
	auctionHave, err := a.loadMarketStock(a.cfg.AuctionDB, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	ceraHave, err := a.loadMarketStock(a.cfg.CeraDB, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	return occ, auctionHave, ceraHave, nil
}

func (a *App) loadMarketStock(dbName string, occ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	query := "SELECT owner_id,item_id,COUNT(*) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE owner_id >= ? GROUP BY owner_id,item_id"
	rows, err := a.db.Query(query, a.cfg.SystemOwner.IDBase)
	if err != nil {
		if isMissingTable(err) {
			return out, nil
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID, itemID uint32
		var count int
		if err := rows.Scan(&ownerID, &itemID, &count); err != nil {
			return nil, err
		}
		occ[ownerID] += count
		out[itemID] += count
	}
	return out, rows.Err()
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "unknown database") || strings.Contains(msg, "no such table")
}

func quoteIdent(v string) string {
	parts := strings.Split(v, ".")
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}
