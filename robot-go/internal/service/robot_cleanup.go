package service

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (m *RobotManager) CleanupRobots(req RobotCleanupRequest) (RobotCleanupResult, error) {
	if err := m.ensureSchema(); err != nil {
		return RobotCleanupResult{}, err
	}
	candidates, err := m.cleanupCandidates(req)
	if err != nil {
		return RobotCleanupResult{}, err
	}
	result := RobotCleanupResult{DryRun: !req.Force, Requested: len(candidates), Candidates: candidates}
	if !req.Force {
		for _, c := range candidates {
			if c.Protected {
				result.Skipped++
			}
		}
		return result, nil
	}
	deleteIndexes := make([]int, 0, len(candidates))
	uids := make([]int, 0, len(candidates))
	cids := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if c.Protected {
			result.Skipped++
			continue
		}
		deleteIndexes = append(deleteIndexes, i)
		uids = append(uids, c.UID)
		if c.CID > 0 {
			cids = append(cids, c.CID)
		}
	}
	if len(uids) > 0 {
		if supervisor := m.currentSupervisor(); supervisor != nil {
			for _, uid := range uids {
				supervisor.StopUID(uid, false)
			}
		}
		_, _ = m.Logout(RobotCommandRequest{UIDs: uids})
		time.Sleep(5 * time.Second)
		m.autoMu.Lock()
		for _, uid := range uids {
			delete(m.autoStoreBusy, uid)
		}
		m.autoMu.Unlock()
		if err := m.batchDeleteRobotData(uids, cids); err != nil {
			for _, i := range deleteIndexes {
				result.Candidates[i].Protected = true
				result.Candidates[i].Reason = err.Error()
				result.Skipped++
			}
			return result, nil
		}
		for _, i := range deleteIndexes {
			result.Candidates[i].Deleted = true
			result.Deleted++
		}
	}
	return result, nil
}

func (m *RobotManager) cleanupCandidates(req RobotCleanupRequest) ([]RobotCleanupCandidate, error) {
	var rows *sql.Rows
	var err error
	if len(req.UIDs) > 0 {
		holders := strings.TrimRight(strings.Repeat("?,", len(req.UIDs)), ",")
		args := make([]interface{}, len(req.UIDs))
		for i, uid := range req.UIDs {
			args[i] = uid
		}
		rows, err = m.db.Query("SELECT r.uid,r.cid,r.charac_name,r.account,IFNULL(a.accountname,''),IFNULL(d.UID,'') FROM d_starsky.robot_registry r LEFT JOIN d_taiwan.accounts a ON a.UID=r.uid LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid WHERE r.uid IN ("+holders+") ORDER BY r.uid", args...)
	} else if req.MinUID > 0 || req.MaxUID > 0 {
		if req.MaxUID <= 0 {
			req.MaxUID = req.MinUID
		}
		if req.MinUID <= 0 {
			req.MinUID = req.MaxUID
		}
		if req.MinUID > req.MaxUID {
			req.MinUID, req.MaxUID = req.MaxUID, req.MinUID
		}
		rows, err = m.db.Query("SELECT r.uid,r.cid,r.charac_name,r.account,IFNULL(a.accountname,''),IFNULL(d.UID,'') FROM d_starsky.robot_registry r LEFT JOIN d_taiwan.accounts a ON a.UID=r.uid LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid WHERE r.uid BETWEEN ? AND ? ORDER BY r.uid", req.MinUID, req.MaxUID)
	} else {
		rows, err = m.db.Query("SELECT r.uid,r.cid,r.charac_name,r.account,IFNULL(a.accountname,''),IFNULL(d.UID,'') FROM d_starsky.robot_registry r LEFT JOIN d_taiwan.accounts a ON a.UID=r.uid LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid ORDER BY r.uid")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RobotCleanupCandidate
	seen := make(map[int]bool)
	for rows.Next() {
		var c RobotCleanupCandidate
		var accountName, dummyUID string
		if err := rows.Scan(&c.UID, &c.CID, &c.Name, &c.Account, &accountName, &dummyUID); err != nil {
			return nil, err
		}
		seen[c.UID] = true
		expected := fmt.Sprintf("%d", c.UID)
		if accountName != expected {
			c.Protected = true
			c.Reason = "accountname does not equal uid"
		} else if dummyUID == "" {
			c.Protected = true
			c.Reason = "missing Dummylist row"
		} else if c.Account != expected {
			c.Protected = true
			c.Reason = "registry account does not equal uid"
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if req.MinUID > 0 || req.MaxUID > 0 {
		orphans, err := m.orphanStorePermissionCandidates(req.MinUID, req.MaxUID, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, orphans...)
	}
	return out, nil
}

func (m *RobotManager) orphanStorePermissionCandidates(minUID, maxUID int, seen map[int]bool) ([]RobotCleanupCandidate, error) {
	if maxUID <= 0 {
		maxUID = minUID
	}
	if minUID <= 0 {
		minUID = maxUID
	}
	if minUID > maxUID {
		minUID, maxUID = maxUID, minUID
	}
	rows, err := m.db.Query(`
SELECT x.uid,IFNULL(a.accountname,'')
FROM (
  SELECT m_id AS uid FROM taiwan_login.member_premium WHERE m_id BETWEEN ? AND ?
  UNION SELECT UID AS uid FROM d_starsky.Robot_stall WHERE UID BETWEEN ? AND ?
  UNION SELECT UID AS uid FROM d_starsky.Robot_stall_config WHERE UID BETWEEN ? AND ?
) x
LEFT JOIN d_starsky.robot_registry r ON r.uid=x.uid
LEFT JOIN d_taiwan.accounts a ON a.UID=x.uid
WHERE r.uid IS NULL
ORDER BY x.uid`, minUID, maxUID, minUID, maxUID, minUID, maxUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RobotCleanupCandidate
	for rows.Next() {
		var uid int
		var accountName string
		if err := rows.Scan(&uid, &accountName); err != nil {
			return nil, err
		}
		if seen[uid] {
			continue
		}
		seen[uid] = true
		c := RobotCleanupCandidate{UID: uid, CID: 0, Name: "orphan-store-permission", Account: fmt.Sprintf("%d", uid)}
		if accountName != "" && accountName != c.Account {
			c.Protected = true
			c.Reason = "accountname does not equal uid"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (m *RobotManager) batchDeleteRobotData(uids, cids []int) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	uidTables := map[string]string{
		"d_starsky.Dummylist":                 "UID",
		"d_starsky.v4_ai_user":                "uid",
		"d_starsky.robot_registry":            "uid",
		"d_starsky.Robot_stall":               "UID",
		"d_starsky.Robot_stall_config":        "UID",
		"d_taiwan.accounts":                   "UID",
		"d_taiwan.limit_create_character":     "m_id",
		"d_taiwan.member_info":                "m_id",
		"d_taiwan.member_info_bot_backup":     "m_id",
		"d_taiwan.member_miles":               "m_id",
		"d_taiwan.member_white_account":       "m_id",
		"taiwan_login.allow_proxy_user":       "m_id",
		"taiwan_login.churn_member_info":      "m_id",
		"taiwan_login.login_account_3":        "m_id",
		"taiwan_login.member_play_info":       "m_id",
		"taiwan_login.member_login":           "m_id",
		"taiwan_login.member_game_option":     "m_id",
		"taiwan_login.member_premium":         "m_id",
		"taiwan_login.dnf_event_entry":        "m_id",
		"taiwan_prod.prod_buy_user":           "m_id",
		"taiwan_prod.pu_user_list":            "m_id",
		"taiwan_login_play.member_key_option": "m_id",
		"taiwan_cain.charac_view":             "m_id",
		"taiwan_cain.charac_link_message":     "m_id",
		"taiwan_cain.member_dungeon":          "m_id",
	}
	for table, col := range uidTables {
		if err := m.batchDeleteByInts(tx, table, col, uids); err != nil {
			return err
		}
	}
	accounts := make([]string, len(uids))
	for i, uid := range uids {
		accounts[i] = fmt.Sprintf("%d", uid)
	}
	if err := m.batchDeleteByStrings(tx, "taiwan_billing.cash_cera", "account", accounts); err != nil {
		return err
	}
	if err := m.batchDeleteByStrings(tx, "taiwan_billing.cash_cera_point", "account", accounts); err != nil {
		return err
	}
	cidTables := []string{
		"taiwan_cain.charac_info", "taiwan_cain.charac_stat", "taiwan_cain.charac_kill_monster_info", "taiwan_cain.charac_npc", "taiwan_cain.new_charac_quest", "taiwan_cain.pvp_result",
		"taiwan_cain_2nd.charac_inven_expand", "taiwan_cain_2nd.inventory", "taiwan_cain_2nd.skill", "taiwan_cain_2nd.user_items",
	}
	for _, table := range cidTables {
		if err := m.batchDeleteByInts(tx, table, "charac_no", cids); err != nil {
			return err
		}
	}
	if err := m.batchDeleteByInts(tx, "taiwan_game_event.event_1306_account_reward", "m_id", uids); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *RobotManager) batchDeleteByInts(tx *sql.Tx, table, col string, ids []int) error {
	cols, err := m.tableColumns(table)
	if err != nil || !cols[col] {
		return nil
	}
	for i := 0; i < len(ids); i += 500 {
		end := i + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		holders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]interface{}, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}
		if _, err := tx.Exec("DELETE FROM "+quoteTable(table)+" WHERE `"+col+"` IN ("+holders+")", args...); err != nil {
			return err
		}
	}
	return nil
}

func (m *RobotManager) batchDeleteByStrings(tx *sql.Tx, table, col string, values []string) error {
	cols, err := m.tableColumns(table)
	if err != nil || !cols[col] {
		return nil
	}
	for i := 0; i < len(values); i += 500 {
		end := i + 500
		if end > len(values) {
			end = len(values)
		}
		chunk := values[i:end]
		holders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]interface{}, len(chunk))
		for j, v := range chunk {
			args[j] = v
		}
		if _, err := tx.Exec("DELETE FROM "+quoteTable(table)+" WHERE `"+col+"` IN ("+holders+")", args...); err != nil {
			return err
		}
	}
	return nil
}
