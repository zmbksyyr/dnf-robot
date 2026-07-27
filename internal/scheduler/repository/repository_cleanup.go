package repository

import (
	"database/sql"
	"fmt"
	robotcap "robot/internal/capability/robot"
	foundsql "robot/internal/foundation/sql"
)

func (r *SQLRepository) CleanupCandidates(req robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, error) {
	candidates, seen, err := r.cleanupRegisteredCandidates(req)
	if err != nil {
		return nil, err
	}
	if req.InternalConfirmedBroken && len(req.UIDs) > 0 {
		for _, uid := range req.UIDs {
			if uid <= 0 || seen[uid] {
				continue
			}
			seen[uid] = true
			account := fmt.Sprintf("%d", uid)
			c := robotcap.CleanupCandidate{
				UID:     uid,
				CID:     0,
				Name:    "confirmed-broken",
				Account: account,
				Reason:  "confirmed broken uid without registry row",
			}
			accountName, err := r.accountName(uid)
			if err != nil {
				return nil, err
			}
			if accountName != "" && accountName != account {
				c.Protected = true
				c.Reason = "accountname does not equal uid"
			}
			candidates = append(candidates, c)
		}
	}
	if req.MinUID > 0 || req.MaxUID > 0 {
		orphans, err := r.orphanStorePermissionCandidates(req.MinUID, req.MaxUID, seen)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, orphans...)
	}
	return candidates, nil
}

func (r *SQLRepository) cleanupRegisteredCandidates(req robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, map[int]bool, error) {
	query := `SELECT r.uid,r.cid,r.charac_name,r.account,IF(a.UID IS NULL,0,1),a.accountname,
IFNULL(d.UID,''),
IF(EXISTS(SELECT 1 FROM taiwan_cain.charac_info c WHERE c.delete_flag=0 AND c.charac_no=r.cid)
OR EXISTS(SELECT 1 FROM taiwan_cain.charac_info c WHERE c.delete_flag=0 AND c.m_id=r.uid),1,0)
FROM d_starsky.robot_registry r
LEFT JOIN d_taiwan.accounts a ON a.UID=r.uid
LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid`
	var rows *sql.Rows
	var err error
	if len(req.UIDs) > 0 {
		holders := foundsql.Placeholders(len(req.UIDs))
		args := make([]interface{}, len(req.UIDs))
		for i, uid := range req.UIDs {
			args[i] = uid
		}
		rows, err = r.Query(query+" WHERE r.uid IN ("+holders+") ORDER BY r.uid", args...)
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
		rows, err = r.Query(query+" WHERE r.uid BETWEEN ? AND ? ORDER BY r.uid", req.MinUID, req.MaxUID)
	} else {
		rows, err = r.Query(query + " ORDER BY r.uid")
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []robotcap.CleanupCandidate
	seen := make(map[int]bool)
	for rows.Next() {
		var c robotcap.CleanupCandidate
		var accountName, dummyUID sql.NullString
		var accountExists, coreExists int
		if err := rows.Scan(&c.UID, &c.CID, &c.Name, &c.Account, &accountExists, &accountName, &dummyUID, &coreExists); err != nil {
			return nil, nil, err
		}
		seen[c.UID] = true
		classifyRegisteredCleanupCandidate(&c, accountExists != 0, accountName, dummyUID.String != "", coreExists != 0, req.InternalConfirmedBroken)
		out = append(out, c)
	}
	return out, seen, rows.Err()
}

func classifyRegisteredCleanupCandidate(c *robotcap.CleanupCandidate, accountExists bool, accountName sql.NullString, dummyExists, coreExists, confirmedBroken bool) {
	expected := fmt.Sprintf("%d", c.UID)
	registryMatches := c.Account == expected
	accountMatches := accountExists && accountName.Valid && accountName.String == expected
	if accountExists && !accountMatches {
		c.Protected = true
		c.Reason = "accountname does not equal uid"
	} else if !accountExists && !coreExists && registryMatches {
		c.MetadataOnly = true
		c.Reason = "account and character missing; robot metadata only"
	} else if !accountExists && coreExists {
		c.Protected = true
		c.Reason = "account missing but character data exists"
	} else if !dummyExists && !confirmedBroken {
		c.Protected = true
		c.Reason = "missing Dummylist row"
	} else if !registryMatches || !accountMatches {
		c.Protected = true
		c.Reason = "registry account does not equal uid"
	}
}

func (r *SQLRepository) accountName(uid int) (string, error) {
	var accountName sql.NullString
	err := r.QueryRow("SELECT accountname FROM d_taiwan.accounts WHERE UID=? LIMIT 1", uid).Scan(&accountName)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !accountName.Valid {
		return "", nil
	}
	return accountName.String, nil
}

func (r *SQLRepository) orphanStorePermissionCandidates(minUID, maxUID int, seen map[int]bool) ([]robotcap.CleanupCandidate, error) {
	if maxUID <= 0 {
		maxUID = minUID
	}
	if minUID <= 0 {
		minUID = maxUID
	}
	if minUID > maxUID {
		minUID, maxUID = maxUID, minUID
	}
	rows, err := r.Query(`
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
	var out []robotcap.CleanupCandidate
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
		c := robotcap.CleanupCandidate{UID: uid, CID: 0, Name: "orphan-store-permission", Account: fmt.Sprintf("%d", uid)}
		if accountName != "" && accountName != c.Account {
			c.Protected = true
			c.Reason = "accountname does not equal uid"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLRepository) BatchDeleteRobotData(uids, cids []int) error {
	tx, err := r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	uidTables := map[string]string{
		"d_starsky.Dummylist":                 "UID",
		"d_starsky.v4_ai_user":                "uid",
		"d_starsky.robot_store_role":          "uid",
		"d_starsky.robot_registry":            "uid",
		"d_starsky.Robot_stall":               "UID",
		"d_starsky.Robot_stall_config":        "UID",
		"d_taiwan.accounts":                   "UID",
		"d_taiwan.limit_create_character":     "m_id",
		"d_taiwan.member_info":                "m_id",
		"d_taiwan.member_info_bot_backup":     "m_id",
		"d_taiwan.member_miles":               "m_id",
		"d_taiwan.member_punish_info":         "m_id",
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
		if err := r.batchDeleteByInts(tx, table, col, uids); err != nil {
			return err
		}
	}
	accounts := make([]string, len(uids))
	for i, uid := range uids {
		accounts[i] = fmt.Sprintf("%d", uid)
	}
	if err := r.batchDeleteByStrings(tx, "taiwan_billing.cash_cera", "account", accounts); err != nil {
		return err
	}
	if err := r.batchDeleteByStrings(tx, "taiwan_billing.cash_cera_point", "account", accounts); err != nil {
		return err
	}
	cidTables := []string{
		"taiwan_cain.charac_info", "taiwan_cain.charac_stat", "taiwan_cain.charac_expert_job", "taiwan_cain.charac_kill_monster_info", "taiwan_cain.charac_npc", "taiwan_cain.new_charac_quest", "taiwan_cain.pvp_result",
		"taiwan_cain_2nd.charac_inven_expand", "taiwan_cain_2nd.inventory", "taiwan_cain_2nd.skill", "taiwan_cain_2nd.user_items",
	}
	for _, table := range cidTables {
		if err := r.batchDeleteByInts(tx, table, "charac_no", cids); err != nil {
			return err
		}
	}
	if err := r.batchDeleteByInts(tx, "taiwan_game_event.event_1306_account_reward", "m_id", uids); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) BatchDeleteRobotMetadata(uids []int) error {
	if len(uids) == 0 {
		return nil
	}
	tables := []struct {
		name string
		col  string
	}{
		{name: "d_starsky.Dummylist", col: "UID"},
		{name: "d_starsky.v4_ai_user", col: "uid"},
		{name: "d_starsky.robot_store_role", col: "uid"},
		{name: "d_starsky.Robot_stall", col: "UID"},
		{name: "d_starsky.Robot_stall_config", col: "UID"},
		{name: "d_starsky.robot_registry", col: "uid"},
	}
	present := make([]struct {
		name string
		col  string
	}, 0, len(tables))
	for _, table := range tables {
		exists, err := r.TableExists(table.name)
		if err != nil {
			return err
		}
		if exists {
			present = append(present, table)
		}
	}
	tx, err := r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range present {
		if err := r.batchDeleteByInts(tx, table.name, table.col, uids); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLRepository) batchDeleteByInts(tx *sql.Tx, table, col string, ids []int) error {
	cols, err := r.TableColumns(table)
	if err != nil {
		return err
	}
	if !cols[col] {
		return nil
	}
	for i := 0; i < len(ids); i += 500 {
		end := i + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		holders := foundsql.Placeholders(len(chunk))
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

func (r *SQLRepository) batchDeleteByStrings(tx *sql.Tx, table, col string, values []string) error {
	cols, err := r.TableColumns(table)
	if err != nil {
		return err
	}
	if !cols[col] {
		return nil
	}
	for i := 0; i < len(values); i += 500 {
		end := i + 500
		if end > len(values) {
			end = len(values)
		}
		chunk := values[i:end]
		holders := foundsql.Placeholders(len(chunk))
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
