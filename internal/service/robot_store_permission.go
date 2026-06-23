package service

import (
	"fmt"
	"strconv"
)

func (m *RobotManager) ensureStorePermission(uid, cid int) error {
	if uid <= 0 || cid <= 0 {
		return nil
	}
	if err := m.repairRobotExpBounds(uid, cid); err != nil {
		return err
	}
	if err := m.disableRobotTradePunish(uid); err != nil {
		return err
	}
	steps := []struct {
		query string
		args  []interface{}
	}{
		{"DELETE FROM taiwan_login.member_premium WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_login.member_premium(pre_type,m_id,service_start,service_end,event_id,server_id) VALUES(8,?,NOW(),'2030-12-31 00:00:00',50002,0)", []interface{}{uid}},
		{"DELETE FROM d_taiwan.member_miles WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO d_taiwan.member_miles(m_id,miles) VALUES(?,7)", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.prod_buy_user WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_prod.prod_buy_user(m_id,user_id,sex,birthday,first_buy_time,last_buy_time) VALUES(?,?,0,'',NOW(),NOW())", []interface{}{uid, strconv.Itoa(uid)}},
		{"DELETE FROM taiwan_prod.pu_user_list WHERE m_id=?", []interface{}{uid}},
		{"INSERT INTO taiwan_prod.pu_user_list(m_id) VALUES(?)", []interface{}{uid}},
		{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=? AND charac_no=?", []interface{}{uid, cid}},
		{"INSERT INTO taiwan_login.dnf_event_entry(event_id,m_id,occ_date,server_id,charac_no,obtain_date) VALUES(50002,?,NOW(),0,?,NOW())", []interface{}{uid, cid}},
	}
	for _, step := range steps {
		if _, err := m.db.Exec(step.query, step.args...); err != nil {
			return err
		}
	}
	var premium, miles, prodUser, puUser, eventEntry int
	checks := []struct {
		dst   *int
		query string
		args  []interface{}
	}{
		{&premium, "SELECT COUNT(*) FROM taiwan_login.member_premium WHERE event_id=50002 AND pre_type=8 AND m_id=? AND service_end>NOW()", []interface{}{uid}},
		{&miles, "SELECT COUNT(*) FROM d_taiwan.member_miles WHERE m_id=? AND miles>=7", []interface{}{uid}},
		{&prodUser, "SELECT COUNT(*) FROM taiwan_prod.prod_buy_user WHERE m_id=?", []interface{}{uid}},
		{&puUser, "SELECT COUNT(*) FROM taiwan_prod.pu_user_list WHERE m_id=?", []interface{}{uid}},
		{&eventEntry, "SELECT COUNT(*) FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=? AND charac_no=?", []interface{}{uid, cid}},
	}
	for _, check := range checks {
		if err := m.db.QueryRow(check.query, check.args...).Scan(check.dst); err != nil {
			return fmt.Errorf("verify store permission uid=%d cid=%d: %w", uid, cid, err)
		}
	}
	robotLogf("[StorePrepare] uid=%d cid=%d permission premium=%d miles=%d prod_user=%d pu_user=%d event_entry=%d\n",
		uid, cid, premium, miles, prodUser, puUser, eventEntry)
	return nil
}

func (m *RobotManager) disableRobotTradePunish(uid int) error {
	exists, err := m.tableExists("d_taiwan.member_punish_info")
	if err != nil || !exists {
		return err
	}
	res, err := m.db.Exec("DELETE FROM d_taiwan.member_punish_info WHERE m_id=? AND punish_type=11", uid)
	if err == nil {
		if rows, rowErr := res.RowsAffected(); rowErr == nil && rows > 0 {
			robotLogf("[StorePrepare] uid=%d cleared_trade_punish rows=%d\n", uid, rows)
		}
	}
	return err
}

func (m *RobotManager) repairRobotExpBounds(uid, cid int) error {
	if uid <= 0 || cid <= 0 {
		return nil
	}
	exists, err := m.tableExists("taiwan_cain.exp_level_ref")
	if err != nil {
		return err
	}
	var refRows int
	if exists {
		_ = m.db.QueryRow("SELECT COUNT(*) FROM taiwan_cain.exp_level_ref").Scan(&refRows)
	}
	var changed int64
	if refRows > 0 {
		res, err := m.db.Exec(`UPDATE taiwan_cain.charac_info c
			JOIN taiwan_cain.exp_level_ref e ON e.lev=c.lev
			LEFT JOIN taiwan_cain.exp_level_ref n ON n.lev=c.lev+1
			SET c.exp=e.exp
			WHERE c.charac_no=? AND (c.exp<e.exp OR (n.exp IS NOT NULL AND c.exp>=n.exp))`, cid)
		if err != nil {
			return err
		}
		if rows, rowErr := res.RowsAffected(); rowErr == nil {
			changed += rows
		}
		res, err = m.db.Exec(`UPDATE taiwan_cain.charac_stat s
			JOIN taiwan_cain.charac_info c ON c.charac_no=s.charac_no
			JOIN taiwan_cain.exp_level_ref e ON e.lev=c.lev
			LEFT JOIN taiwan_cain.exp_level_ref n ON n.lev=c.lev+1
			SET s.exp=e.exp
			WHERE s.charac_no=? AND (s.exp<e.exp OR (n.exp IS NOT NULL AND s.exp>=n.exp))`, cid)
		if err != nil {
			return err
		}
		if rows, rowErr := res.RowsAffected(); rowErr == nil {
			changed += rows
		}
	} else {
		var lev, infoExp, statExp int
		if err := m.db.QueryRow(`SELECT IFNULL(c.lev,0),IFNULL(c.exp,0),IFNULL(s.exp,0)
			FROM taiwan_cain.charac_info c LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=c.charac_no
			WHERE c.charac_no=?`, cid).Scan(&lev, &infoExp, &statExp); err != nil {
			return err
		}
		minExp, ok := robotLevelMinExp(lev)
		if ok && (infoExp < minExp || statExp < minExp) {
			res, err := m.db.Exec("UPDATE taiwan_cain.charac_info SET exp=? WHERE charac_no=? AND exp<?", minExp, cid, minExp)
			if err != nil {
				return err
			}
			if rows, rowErr := res.RowsAffected(); rowErr == nil {
				changed += rows
			}
			res, err = m.db.Exec("UPDATE taiwan_cain.charac_stat SET exp=? WHERE charac_no=? AND exp<?", minExp, cid, minExp)
			if err != nil {
				return err
			}
			if rows, rowErr := res.RowsAffected(); rowErr == nil {
				changed += rows
			}
		}
	}
	if changed > 0 {
		robotLogf("[StorePrepare] uid=%d cid=%d repaired_exp_bounds ref_rows=%d rows=%d\n", uid, cid, refRows, changed)
	}
	return nil
}

func robotLevelMinExp(level int) (int, bool) {
	if level < 1 || level >= len(robotLevelMinExpTable) {
		return 0, false
	}
	return robotLevelMinExpTable[level], true
}

var robotLevelMinExpTable = []int{
	0,
	0, 1000, 2653, 5543, 10575, 18509, 30205, 46627, 68840, 98012,
	135412, 182411, 240483, 311203, 396249, 497399, 619844, 767003, 942667, 1150141,
	1393864, 1677655, 2016592, 2419616, 2881880, 3410208, 4010357, 4690036, 5457538, 6319795,
	7286075, 8364071, 9564081, 10897068, 12372076, 14001186, 15794305, 17764684, 19926329, 22290671,
	24872971, 27685628, 30844672, 34385491, 38223728, 42379527, 46869144, 51714280, 56937632, 62557467,
	68598134, 75079161, 82244762, 90154153, 98612376, 107650320, 117292652, 127572249, 138523258, 150172894,
	162557394, 175705561, 190075639, 205759433, 222370655, 239953967, 258544759, 278190155, 298938852, 320829378,
	343912998, 368230186, 394571074, 423070174, 453854220, 487070095, 522855717, 561371046, 602783706, 647250436,
	694953116, 746061309, 801975949, 863016468, 929492724,
}

func (m *RobotManager) revokeStorePermission(uid, cid int) error {
	if uid <= 0 {
		return nil
	}
	steps := []struct {
		query string
		args  []interface{}
	}{
		{"DELETE FROM d_starsky.Robot_stall WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"DELETE FROM d_starsky.Robot_stall_config WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"UPDATE d_starsky.Dummylist SET function_type=0 WHERE UID=?", []interface{}{uid}},
	}
	for _, step := range steps {
		if _, err := m.db.Exec(step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}
