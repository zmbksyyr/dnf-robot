package service

import (
	"strconv"
)

func (m *RobotManager) ensureStorePermission(uid, cid int) error {
	if uid <= 0 || cid <= 0 {
		return nil
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
	return nil
}

func (m *RobotManager) revokeStorePermission(uid, cid int) error {
	if uid <= 0 {
		return nil
	}
	steps := []struct {
		query string
		args  []interface{}
	}{
		{"DELETE FROM taiwan_login.member_premium WHERE pre_type=8 AND m_id=?", []interface{}{uid}},
		{"DELETE FROM d_taiwan.member_miles WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.prod_buy_user WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM taiwan_prod.pu_user_list WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM d_starsky.Robot_stall WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"DELETE FROM d_starsky.Robot_stall_config WHERE UID=? AND function_type=2", []interface{}{uid}},
		{"UPDATE d_starsky.Dummylist SET function_type=0 WHERE UID=?", []interface{}{uid}},
	}
	if cid > 0 {
		steps = append(steps, struct {
			query string
			args  []interface{}
		}{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=? AND charac_no=?", []interface{}{uid, cid}})
	} else {
		steps = append(steps, struct {
			query string
			args  []interface{}
		}{"DELETE FROM taiwan_login.dnf_event_entry WHERE event_id=50002 AND m_id=?", []interface{}{uid}})
	}
	for _, step := range steps {
		if _, err := m.db.Exec(step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}
