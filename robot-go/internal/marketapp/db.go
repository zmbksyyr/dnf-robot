package marketapp

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var mysqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (a *App) ensureMarketTables(now time.Time) ([]string, error) {
	seen := map[string]bool{}
	var ensured []string
	for _, dbName := range []string{a.cfg.AuctionDB, a.cfg.CeraDB} {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" || seen[dbName] {
			continue
		}
		seen[dbName] = true
		tables, err := a.ensureAuctionMonthlyTables(dbName, now)
		ensured = append(ensured, tables...)
		if err != nil {
			return ensured, err
		}
	}
	return ensured, nil
}

func (a *App) ensureAuctionMonthlyTables(dbName string, now time.Time) ([]string, error) {
	if !mysqlIdentifierPattern.MatchString(dbName) {
		return nil, fmt.Errorf("invalid auction db %q", dbName)
	}
	yyyymm := now.Format("200601")
	targets := []struct {
		base string
		name string
	}{
		{base: "auction_history", name: "auction_history_" + yyyymm},
		{base: "auction_history_buyer", name: "auction_history_buyer_" + yyyymm},
	}
	created := make([]string, 0, len(targets))
	for _, target := range targets {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` LIKE `%s`.`%s`", dbName, target.name, dbName, target.base)
		if _, err := a.db.Exec(query); err != nil {
			return created, fmt.Errorf("ensure monthly table %s.%s: %w", dbName, target.name, err)
		}
		created = append(created, dbName+"."+target.name)
	}
	return created, nil
}

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
