package marketapp

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const specialAddInfoBase int32 = 210000000
const maxInt32 int32 = 2147483647

var mysqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (a *App) marketDBNames() []string {
	return []string{a.cfg.AuctionDB, a.cfg.CeraDB}
}

func (r SQLRepository) EnsureMarketTables(dbNames []string, now time.Time) ([]string, error) {
	seen := map[string]bool{}
	var ensured []string
	for _, dbName := range dbNames {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" || seen[dbName] {
			continue
		}
		seen[dbName] = true
		tables, err := r.ensureAuctionMonthlyTables(dbName, now)
		ensured = append(ensured, tables...)
		if err != nil {
			return ensured, err
		}
	}
	return ensured, nil
}

func (r SQLRepository) ensureAuctionMonthlyTables(dbName string, now time.Time) ([]string, error) {
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
		if _, err := r.db.Exec(query); err != nil {
			return created, fmt.Errorf("ensure monthly table %s.%s: %w", dbName, target.name, err)
		}
		created = append(created, dbName+"."+target.name)
	}
	return created, nil
}

func (a *App) loadSystemStock() (map[uint32]int, map[uint32]int, map[uint32]int, error) {
	occ := map[uint32]int{}
	auctionHave, err := a.repository.LoadMarketStock(a.cfg.AuctionDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	ceraHave, err := a.repository.LoadMarketStock(a.cfg.CeraDB, a.cfg.SystemOwner.IDBase, occ)
	if err != nil {
		return nil, nil, nil, err
	}
	return occ, auctionHave, ceraHave, nil
}

func (r SQLRepository) LoadMarketStock(dbName string, systemOwnerBase uint32, occ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	query := "SELECT owner_id,item_id,COUNT(*) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE owner_id >= ? GROUP BY owner_id,item_id"
	rows, err := r.db.Query(query, systemOwnerBase)
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

func (r SQLRepository) LoadMaxAddInfo(dbName string, min int32) (int32, error) {
	query := "SELECT IFNULL(MAX(add_info),0) FROM " + quoteIdent(dbName) + ".`auction_main` WHERE add_info >= ?"
	var max sql.NullInt64
	if err := r.db.QueryRow(query, min).Scan(&max); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	if !max.Valid || max.Int64 <= 0 {
		return 0, nil
	}
	if max.Int64 > int64(maxInt32) {
		return maxInt32, nil
	}
	return int32(max.Int64), nil
}

func (r SQLRepository) CreateCreatureItem(dbName string, ownerID uint32, itemID uint32) (int32, error) {
	table := quoteIdent(dbName) + ".`creature_items`"
	queries := []string{
		"INSERT INTO " + table + " " +
			"(`charac_no`,`slot`,`it_id`,`reg_date`,`name`,`stomach`,`exp`,`endurance`,`creature_type`,`creature_level`,`item_lock`,`delete_flag`,`skills`,`expire_time`,`item_creature_expire_time`) " +
			"VALUES (?,0,?,NOW(),'',100,0,0,0,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')",
		"INSERT INTO " + table + " " +
			"(`charac_no`,`slot`,`it_id`,`reg_date`,`name`,`stomach`,`exp`,`endurance`,`creature_type`,`no_charge`,`stat`,`item_lock_key`,`ipg_agency_no`,`expire_date`,`delete_date`) " +
			"VALUES (?,0,?,NOW(),'',100,0,0,0,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')",
	}
	var lastErr error
	for _, query := range queries {
		id, err := r.insertCreatureItem(query, ownerID, itemID)
		if err == nil {
			return id, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

func (r SQLRepository) insertCreatureItem(query string, ownerID uint32, itemID uint32) (int32, error) {
	result, err := r.db.Exec(query, ownerID, itemID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if id <= 0 || id > int64(maxInt32) {
		return 0, fmt.Errorf("creature ui_id out of range: %d", id)
	}
	return int32(id), nil
}

func (r SQLRepository) CountSystemCreatureItems(dbName string, systemOwnerBase uint32) (int, error) {
	query := "SELECT COUNT(*) FROM " + quoteIdent(dbName) + ".`creature_items` WHERE `charac_no` >= ?"
	var count int
	if err := r.db.QueryRow(query, systemOwnerBase).Scan(&count); err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (r SQLRepository) DeleteSystemCreatureItems(dbName string, systemOwnerBase uint32) (int64, error) {
	query := "DELETE FROM " + quoteIdent(dbName) + ".`creature_items` WHERE `charac_no` >= ?"
	result, err := r.db.Exec(query, systemOwnerBase)
	if err != nil {
		if isMissingTable(err) {
			return 0, nil
		}
		return 0, err
	}
	return result.RowsAffected()
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
