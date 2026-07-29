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
	legacy, err := r.cleanupLegacyDummyCandidates(req, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, legacy...)
	viewOrphans, err := r.orphanCharacViewCandidates(req, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, viewOrphans...)
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
			accountName, characterCount, err := r.unregisteredCleanupIdentity(uid)
			if err != nil {
				return nil, err
			}
			classifyUnregisteredCleanupCandidate(&c, accountName, characterCount)
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

// orphanCharacViewCandidates finds charac_view rows left behind after a
// creation/cleanup failure. They are safe to remove only when no account,
// character, or robot registry row still owns the UID.
func (r *SQLRepository) orphanCharacViewCandidates(req robotcap.CleanupRequest, seen map[int]bool) ([]robotcap.CleanupCandidate, error) {
	query := `SELECT CAST(v.m_id AS UNSIGNED)
FROM taiwan_cain.charac_view v
LEFT JOIN d_starsky.robot_registry r ON r.uid=CAST(v.m_id AS UNSIGNED)
LEFT JOIN d_taiwan.accounts a ON a.UID=CAST(v.m_id AS UNSIGNED)
LEFT JOIN taiwan_cain.charac_info c ON c.m_id=CAST(v.m_id AS UNSIGNED)
WHERE r.uid IS NULL AND a.UID IS NULL AND c.m_id IS NULL
AND CAST(v.m_id AS UNSIGNED)>0`
	args := make([]interface{}, 0, len(req.UIDs)+2)
	if len(req.UIDs) > 0 {
		query += " AND CAST(v.m_id AS UNSIGNED) IN (" + foundsql.Placeholders(len(req.UIDs)) + ")"
		for _, uid := range req.UIDs {
			args = append(args, uid)
		}
	} else if req.MinUID > 0 || req.MaxUID > 0 {
		minUID, maxUID := req.MinUID, req.MaxUID
		if maxUID <= 0 {
			maxUID = minUID
		}
		if minUID <= 0 {
			minUID = maxUID
		}
		if minUID > maxUID {
			minUID, maxUID = maxUID, minUID
		}
		query += " AND CAST(v.m_id AS UNSIGNED) BETWEEN ? AND ?"
		args = append(args, minUID, maxUID)
	}
	rows, err := r.Query(query+" ORDER BY CAST(v.m_id AS UNSIGNED)", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []robotcap.CleanupCandidate
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		if uid <= 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, robotcap.CleanupCandidate{
			UID: uid, Name: "orphan-charac-view", Account: fmt.Sprintf("%d", uid),
			Reason: "orphan charac_view metadata without account or character",
		})
	}
	return out, rows.Err()
}

func (r *SQLRepository) cleanupLegacyDummyCandidates(req robotcap.CleanupRequest, seen map[int]bool) ([]robotcap.CleanupCandidate, error) {
	query := `SELECT CAST(d.UID AS UNSIGNED),CAST(d.CID AS UNSIGNED),IFNULL(c.charac_name,''),a.accountname,
IF(c.charac_no IS NULL,0,1),IFNULL(c.m_id,0),
(SELECT COUNT(*) FROM taiwan_cain.charac_info owned WHERE owned.m_id=CAST(d.UID AS UNSIGNED))
FROM d_starsky.Dummylist d
LEFT JOIN d_starsky.robot_registry r ON r.uid=CAST(d.UID AS UNSIGNED)
LEFT JOIN d_taiwan.accounts a ON a.UID=CAST(d.UID AS UNSIGNED)
LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=CAST(d.CID AS UNSIGNED)
WHERE r.uid IS NULL AND d.UID REGEXP '^[0-9]+$' AND CAST(d.UID AS UNSIGNED)>0`
	args := make([]interface{}, 0, len(req.UIDs))
	if len(req.UIDs) > 0 {
		query += " AND CAST(d.UID AS UNSIGNED) IN (" + foundsql.Placeholders(len(req.UIDs)) + ")"
		for _, uid := range req.UIDs {
			args = append(args, uid)
		}
	} else if req.MinUID > 0 || req.MaxUID > 0 {
		minUID, maxUID := req.MinUID, req.MaxUID
		if maxUID <= 0 {
			maxUID = minUID
		}
		if minUID <= 0 {
			minUID = maxUID
		}
		if minUID > maxUID {
			minUID, maxUID = maxUID, minUID
		}
		query += " AND CAST(d.UID AS UNSIGNED) BETWEEN ? AND ?"
		args = append(args, minUID, maxUID)
	}
	rows, err := r.Query(query+" ORDER BY CAST(d.UID AS UNSIGNED)", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []robotcap.CleanupCandidate
	for rows.Next() {
		var c robotcap.CleanupCandidate
		var accountName sql.NullString
		var characterExists, characterOwner, ownerCharacterCount int
		if err := rows.Scan(&c.UID, &c.CID, &c.Name, &accountName, &characterExists, &characterOwner, &ownerCharacterCount); err != nil {
			return nil, err
		}
		if seen[c.UID] {
			continue
		}
		seen[c.UID] = true
		c.Account = fmt.Sprintf("%d", c.UID)
		if c.Name == "" {
			c.Name = "legacy-dummy"
		}
		classifyLegacyDummyCleanupCandidate(&c, accountName, characterExists != 0, characterOwner, ownerCharacterCount)
		out = append(out, c)
	}
	return out, rows.Err()
}

func classifyLegacyDummyCleanupCandidate(
	c *robotcap.CleanupCandidate,
	accountName sql.NullString,
	characterExists bool,
	characterOwner int,
	ownerCharacterCount int,
) {
	expected := fmt.Sprintf("%d", c.UID)
	if accountName.Valid && accountName.String != expected {
		c.Protected = true
		c.Reason = "accountname does not equal uid"
	} else if !accountName.Valid && !characterExists && ownerCharacterCount == 0 {
		c.MetadataOnly = true
		c.Reason = "legacy Dummylist metadata only"
	} else if !accountName.Valid && (characterExists || ownerCharacterCount > 0) {
		c.Protected = true
		c.Reason = "account missing but character data exists"
	} else if characterExists && characterOwner != c.UID {
		c.Protected = true
		c.Reason = "Dummylist cid belongs to another uid"
	} else if ownerCharacterCount > 0 && !characterExists {
		c.Protected = true
		c.Reason = "uid character does not match Dummylist cid"
	} else if ownerCharacterCount > 1 {
		c.Protected = true
		c.Reason = "uid has additional characters outside Dummylist"
	}
}

func (r *SQLRepository) cleanupRegisteredCandidates(req robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, map[int]bool, error) {
	query := `SELECT r.uid,r.cid,r.charac_name,r.account,IF(a.UID IS NULL,0,1),a.accountname,
IF(rc.charac_no IS NULL,0,1),IFNULL(rc.m_id,0),
(SELECT COUNT(*) FROM taiwan_cain.charac_info owned WHERE owned.m_id=r.uid)
FROM d_starsky.robot_registry r
LEFT JOIN d_taiwan.accounts a ON a.UID=r.uid
LEFT JOIN taiwan_cain.charac_info rc ON rc.charac_no=r.cid`
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
		var accountName sql.NullString
		var accountExists, registeredCharacterExists, registeredCharacterOwner, ownerCharacterCount int
		if err := rows.Scan(
			&c.UID, &c.CID, &c.Name, &c.Account, &accountExists, &accountName,
			&registeredCharacterExists, &registeredCharacterOwner, &ownerCharacterCount,
		); err != nil {
			return nil, nil, err
		}
		seen[c.UID] = true
		classifyRegisteredCleanupCandidate(
			&c, accountExists != 0, accountName,
			registeredCharacterExists != 0, registeredCharacterOwner, ownerCharacterCount,
		)
		out = append(out, c)
	}
	return out, seen, rows.Err()
}

func classifyRegisteredCleanupCandidate(
	c *robotcap.CleanupCandidate,
	accountExists bool,
	accountName sql.NullString,
	registeredCharacterExists bool,
	registeredCharacterOwner int,
	ownerCharacterCount int,
) {
	expected := fmt.Sprintf("%d", c.UID)
	registryMatches := c.Account == expected
	accountMatches := accountExists && accountName.Valid && accountName.String == expected
	if accountExists && !accountMatches {
		c.Protected = true
		c.Reason = "accountname does not equal uid"
	} else if !accountExists && !registeredCharacterExists && ownerCharacterCount == 0 && registryMatches {
		c.MetadataOnly = true
		c.Reason = "account and character missing; robot metadata only"
	} else if !accountExists && (registeredCharacterExists || ownerCharacterCount > 0) {
		c.Protected = true
		c.Reason = "account missing but character data exists"
	} else if !registryMatches || !accountMatches {
		c.Protected = true
		c.Reason = "registry account does not equal uid"
	} else if registeredCharacterExists && registeredCharacterOwner != c.UID {
		c.Protected = true
		c.Reason = "registry cid belongs to another uid"
	} else if ownerCharacterCount > 0 && !registeredCharacterExists {
		c.Protected = true
		c.Reason = "uid character does not match registry cid"
	} else if ownerCharacterCount > 1 {
		c.Protected = true
		c.Reason = "uid has additional characters outside registry"
	}
}

func (r *SQLRepository) unregisteredCleanupIdentity(uid int) (sql.NullString, int, error) {
	var accountName sql.NullString
	var characterCount int
	err := r.QueryRow(`SELECT
(SELECT accountname FROM d_taiwan.accounts WHERE UID=? LIMIT 1),
(SELECT COUNT(*) FROM taiwan_cain.charac_info WHERE m_id=?)`, uid, uid).Scan(&accountName, &characterCount)
	if err != nil {
		return sql.NullString{}, 0, err
	}
	return accountName, characterCount, nil
}

func classifyUnregisteredCleanupCandidate(c *robotcap.CleanupCandidate, accountName sql.NullString, characterCount int) {
	expected := fmt.Sprintf("%d", c.UID)
	if accountName.Valid && accountName.String != expected {
		c.Protected = true
		c.Reason = "accountname does not equal uid"
	} else if characterCount > 0 {
		c.Protected = true
		c.Reason = "registry missing but character data exists"
	} else if !accountName.Valid {
		c.MetadataOnly = true
		c.Reason = "account and character missing; robot metadata only"
	}
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
SELECT x.uid,a.accountname,
(SELECT COUNT(*) FROM taiwan_cain.charac_info c WHERE c.m_id=x.uid)
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
		var accountName sql.NullString
		var characterCount int
		if err := rows.Scan(&uid, &accountName, &characterCount); err != nil {
			return nil, err
		}
		if seen[uid] {
			continue
		}
		seen[uid] = true
		c := robotcap.CleanupCandidate{UID: uid, CID: 0, Name: "orphan-store-permission", Account: fmt.Sprintf("%d", uid)}
		classifyUnregisteredCleanupCandidate(&c, accountName, characterCount)
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
		"d_starsky.Dummylist":                     "UID",
		"d_starsky.v4_ai_user":                    "uid",
		"d_starsky.robot_registry":                "uid",
		"d_starsky.Robot_stall":                   "UID",
		"d_starsky.Robot_stall_config":            "UID",
		"d_taiwan.accounts":                       "UID",
		"d_taiwan.limit_create_character":         "m_id",
		"d_taiwan.member_info":                    "m_id",
		"d_taiwan.member_info_bot_backup":         "m_id",
		"d_taiwan.member_join_info":               "m_id",
		"d_taiwan.member_miles":                   "m_id",
		"d_taiwan.member_punish_info":             "m_id",
		"d_taiwan.member_security_grade":          "m_id",
		"d_taiwan.member_white_account":           "m_id",
		"taiwan_login.allow_proxy_user":           "m_id",
		"taiwan_login.churn_member_info":          "m_id",
		"taiwan_login.login_account_3":            "m_id",
		"taiwan_login.member_play_info":           "m_id",
		"taiwan_login.member_login":               "m_id",
		"taiwan_login.member_game_option":         "m_id",
		"taiwan_login.member_join_info":           "m_id",
		"taiwan_login.member_premium":             "m_id",
		"taiwan_login.dnf_event_entry":            "m_id",
		"taiwan_prod.prod_buy_user":               "m_id",
		"taiwan_prod.pu_user_list":                "m_id",
		"taiwan_login_play.member_key_option":     "m_id",
		"taiwan_cain.charac_view":                 "m_id",
		"taiwan_cain.charac_link_message":         "m_id",
		"taiwan_cain.account_cargo":               "m_id",
		"taiwan_cain.member_booster_gage":         "m_id",
		"taiwan_cain.member_dungeon":              "m_id",
		"taiwan_cain_2nd.member_avatar_coin":      "m_id",
		"taiwan_game_event.login_common":          "m_id",
		"taiwan_game_event.mobile_auth_reward_tw": "m_id",
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
		"taiwan_cain.charac_info", "taiwan_cain.charac_stat", "taiwan_cain.charac_achievement", "taiwan_cain.charac_blood_dungeon_reward", "taiwan_cain.charac_blood_inout", "taiwan_cain.charac_dimension_inout", "taiwan_cain.charac_equipment_emblem", "taiwan_cain.charac_expert_job", "taiwan_cain.charac_kill_monster_info", "taiwan_cain.charac_link_bonus", "taiwan_cain.charac_npc", "taiwan_cain.charac_option", "taiwan_cain.charac_quest_shop", "taiwan_cain.charac_titlebook", "taiwan_cain.event_dungeon_clear", "taiwan_cain.new_charac_quest", "taiwan_cain.pvp_result",
		"taiwan_cain_2nd.charac_inven_expand", "taiwan_cain_2nd.creature_items", "taiwan_cain_2nd.fair_pvp_score", "taiwan_cain_2nd.inventory", "taiwan_cain_2nd.skill", "taiwan_cain_2nd.store", "taiwan_cain_2nd.user_items",
		"taiwan_prod.prod_sale_entry_073", "taiwan_prod.prod_sale_entry_162",
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
	ready, err := cleanupTableReady(table, col, r.TableExists, r.TableColumns)
	if err != nil {
		return err
	}
	if !ready {
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
	ready, err := cleanupTableReady(table, col, r.TableExists, r.TableColumns)
	if err != nil {
		return err
	}
	if !ready {
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

func cleanupTableReady(
	table, col string,
	tableExists func(string) (bool, error),
	tableColumns func(string) (map[string]bool, error),
) (bool, error) {
	exists, err := tableExists(table)
	if err != nil || !exists {
		return false, err
	}
	cols, err := tableColumns(table)
	if err != nil {
		return false, err
	}
	return cols[col], nil
}
