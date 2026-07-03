package repository

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	equipcap "robot/internal/capability/equipment"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/foundation/charset"
	"robot/internal/shared"
	"time"
)

func (r *SQLRepository) EnsureAccount(uid int, innerIP string) error {
	now := time.Now().Unix()
	account := fmt.Sprintf("%d", uid)
	steps := []string{
		"CREATE TABLE IF NOT EXISTS d_taiwan.member_info_bot_backup LIKE d_taiwan.member_info",
	}
	for _, q := range steps {
		if _, err := r.Exec(q); err != nil {
			return err
		}
	}
	if _, err := r.ClearTradePunish(uid); err != nil {
		return err
	}
	if err := r.InsertIgnore("d_taiwan.accounts", map[string]interface{}{
		"UID": uid, "accountname": account, "password": "e10adc3949ba59abbe56e057f20f883e",
		"qq": "123456", "VIP": "", "ip": innerIP, "login_IP": "", "login_Mac": "",
		"seal_IP": 0, "seal_MAC": 0, "seal_accountname": 0,
	}); err != nil {
		return err
	}
	upserts := []struct {
		table string
		vals  map[string]interface{}
	}{
		{"d_taiwan.limit_create_character", map[string]interface{}{"m_id": uid, "count": 0, "last_access_time": "0000-00-00 00:00:00"}},
		{"d_taiwan.member_info", map[string]interface{}{"m_id": uid, "user_id": account, "state": 1, "slot": 8, "hangame_flag": 0}},
		{"d_taiwan.member_info_bot_backup", map[string]interface{}{"m_id": uid, "user_id": account, "state": 1, "slot": 8, "hangame_flag": 0}},
		{"d_taiwan.member_miles", map[string]interface{}{"m_id": uid}},
		{"d_taiwan.member_white_account", map[string]interface{}{"m_id": uid}},
		{"taiwan_login.allow_proxy_user", map[string]interface{}{"m_id": uid}},
		{"taiwan_login.churn_member_info", map[string]interface{}{"m_id": uid, "play_info": "000000000000000000000000000011"}},
		{"taiwan_login.login_account_3", map[string]interface{}{"m_id": uid, "m_channel_no": "3011"}},
		{"taiwan_login.member_play_info", map[string]interface{}{"occ_date": time.Now().Format("2006-01-02 15:04:05"), "m_id": uid, "server_id": 1}},
		{"taiwan_billing.cash_cera", map[string]interface{}{"account": account, "cera": 0, "mod_tran": 0, "mod_date": time.Now(), "reg_date": time.Now()}},
		{"taiwan_billing.cash_cera_point", map[string]interface{}{"account": account, "cera_point": 0, "mod_date": time.Now(), "reg_date": time.Now()}},
		{"d_starsky.v4_ai_user", map[string]interface{}{"uid": account, "msg_state": "0", "move_state": "0"}},
	}
	for _, u := range upserts {
		if err := r.InsertIgnore(u.table, u.vals); err != nil {
			return err
		}
	}
	for _, table := range []string{"d_taiwan.member_join_info", "taiwan_login.member_join_info"} {
		if err := r.InsertIgnoreIfTableExists(table, map[string]interface{}{
			"m_id": uid, "reg_date": time.Now().Year(), "ip": innerIP, "contry_code": 0,
			"login_time": now, "error_type": 0, "login_ip": innerIP, "game_use_history": 1,
		}); err != nil {
			return err
		}
	}
	for _, table := range []string{"d_taiwan.member_security_grade", "d_taiwan_secu.member_security_grade"} {
		if err := r.InsertIgnoreIfTableExists(table, map[string]interface{}{"m_id": uid}); err != nil {
			return err
		}
	}
	_, err := r.Exec("INSERT INTO taiwan_login.member_login (m_id,login_time,expire_time,last_play_time,login_ip,cleanpad_point,tutorial_skipable) VALUES (?,?,?,?,?,1,'1') ON DUPLICATE KEY UPDATE login_time=VALUES(login_time),expire_time=2147483647,last_play_time=VALUES(last_play_time),login_ip=VALUES(login_ip),cleanpad_point=1,tutorial_skipable='1'", uid, now, 2147483647, now, innerIP)
	if err != nil {
		return err
	}
	_, _ = r.Exec("INSERT IGNORE INTO taiwan_login.member_game_option VALUES (?,0x48000000789C63646064F85FCFCC90028408F0BF9E9181112C038023042210009AC0C9B,'','',0x10020000789C636018058319686115D5C62AAA83555417ABA81E56517D06003C02010C)", uid)
	_, _ = r.Exec("INSERT IGNORE INTO taiwan_login_play.member_key_option (m_id,key_type,key_option) VALUES (?,0,UNHEX(''))", uid)
	return nil
}

func (r *SQLRepository) ClearTradePunish(uid int) (int64, error) {
	exists, err := r.TableExists("d_taiwan.member_punish_info")
	if err != nil || !exists {
		return 0, err
	}
	res, err := r.Exec("DELETE FROM d_taiwan.member_punish_info WHERE m_id=? AND punish_type=11", uid)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return rows, nil
}

func (r *SQLRepository) CreateBaseCharacter(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	dbName := robottemplate.NameForEncoding(info.Name, "utf8_cp1252")
	if _, err := r.Exec(
		"INSERT INTO taiwan_cain.charac_info (m_id,charac_no,charac_name,village,maxHP,maxMP,phy_attack,phy_defense,mag_attack,mag_defense,inven_weight,hp_regen,mp_regen,move_speed,attack_speed,cast_speed,hit_recovery,jump,charac_weight,max_fatigue,lev,job,grow_type) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		info.UID, info.CID, dbName, info.Village, "1800", "1400", "75", "75", "45", "45", "480000", "0", "500", "8500", "8500", "7000", "6000", "4300", "680000", "156", info.Level, info.Job, info.Grow,
	); err != nil {
		return fmt.Errorf("insert charac_info uid=%d cid=%d: %w", info.UID, info.CID, err)
	}
	if _, err := r.Exec("INSERT INTO taiwan_cain.charac_stat (charac_no,HP,exp,tutorial_flag,village) VALUES (?,?,?,?,?)", info.CID, "100", 0, "-1", info.Village); err != nil {
		return fmt.Errorf("insert charac_stat cid=%d: %w", info.CID, err)
	}
	optional := []struct {
		query string
		args  []interface{}
	}{
		{"INSERT IGNORE INTO taiwan_cain.charac_kill_monster_info (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_cain.charac_link_message (m_id) VALUES (?)", []interface{}{info.UID}},
		{"INSERT IGNORE INTO taiwan_cain.charac_npc (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_cain.member_dungeon (m_id) VALUES (?)", []interface{}{info.UID}},
		{"INSERT IGNORE INTO taiwan_cain.new_charac_quest (charac_no,play_1) VALUES (?,?)", []interface{}{info.CID, "1016"}},
		{"INSERT IGNORE INTO taiwan_cain.pvp_result (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_cain_2nd.charac_inven_expand (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_cain_2nd.inventory (charac_no,money,coin,inventory_capacity,inventory,equipslot) VALUES (?,?,?,?,?,?)", []interface{}{info.CID, rc.DefaultMoney, rc.DefaultCoin, rc.InventoryCapacity, equipcap.CompressedZeros(249 * 61), equipcap.CompressedZeros(12 * 61)}},
		{"INSERT IGNORE INTO taiwan_cain_2nd.skill (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_game_event.event_1306_account_reward (m_id,charac_no,occ_date) VALUES (?,?,NOW())", []interface{}{info.UID, info.CID}},
	}
	for _, q := range optional {
		_, _ = r.Exec(q.query, q.args...)
	}
	return nil
}

func (r *SQLRepository) SaveEquipmentSlots(cid int, raw []byte) error {
	_, err := r.Exec("UPDATE taiwan_cain_2nd.inventory SET equipslot=? WHERE charac_no=?", equipcap.CompressRaw(raw), cid)
	return err
}

func (r *SQLRepository) ReplaceAvatarItems(cid int, selected map[int]shared.EquipmentCatalogItem) error {
	_, _ = r.Exec("DELETE FROM taiwan_cain_2nd.user_items WHERE charac_no=? AND slot<=9", cid)
	for slot, item := range selected {
		_, _ = r.Exec("INSERT INTO taiwan_cain_2nd.user_items (charac_no,slot,it_id,expire_date,reg_date,obtain_from,hidden_option) VALUES (?,?,?,'9999-12-31 23:59:59',NOW(),0,1)", cid, slot, item.ID)
	}
	return nil
}

func (r *SQLRepository) UpsertDummy(info robotcap.Info, innerIP string) error {
	_, err := r.Exec("INSERT INTO d_starsky.Dummylist (ID,YID,UID,port,curvill,curarea,curx,cury,CID,ip,function_type,discost) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE port=VALUES(port),curvill=VALUES(curvill),curarea=VALUES(curarea),curx=VALUES(curx),cury=VALUES(cury),CID=VALUES(CID),ip=VALUES(ip)",
		info.UID, info.UID, info.UID, info.Port, info.Village, info.Area, info.X, info.Y, info.CID, innerIP, "0", "0")
	return err
}

func (r *SQLRepository) RegisterRobot(info robotcap.Info) error {
	account := fmt.Sprintf("%d", info.UID)
	_, err := r.Exec("INSERT INTO d_starsky.robot_registry (uid,cid,account,charac_name,created_at) VALUES (?,?,?,?,NOW()) ON DUPLICATE KEY UPDATE cid=VALUES(cid),account=VALUES(account),charac_name=VALUES(charac_name)",
		info.UID, info.CID, account, info.Name)
	return err
}

func (r *SQLRepository) RebuildCharacView(uid int) error {
	rows, err := r.Query("SELECT charac_no,charac_name,lev,job,grow_type FROM taiwan_cain.charac_info WHERE m_id=? AND delete_flag=0 ORDER BY charac_no", uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cinfo struct {
		cid, lev, job, grow int
		name                string
	}
	var chars []cinfo
	for rows.Next() {
		var c cinfo
		if err := rows.Scan(&c.cid, &c.name, &c.lev, &c.job, &c.grow); err != nil {
			return err
		}
		chars = append(chars, c)
	}
	raw := make([]byte, 36*148)
	for slot, c := range chars {
		if slot >= 36 {
			break
		}
		off := slot * 148
		binary.LittleEndian.PutUint32(raw[off:off+4], uint32(c.cid))
		name := charset.Windows1252StringBytes(c.name)
		if len(name) > 20 {
			name = name[:20]
		}
		copy(raw[off+4:], name)
		raw[off+28] = byte(c.lev)
		raw[off+29] = byte(c.job)
		raw[off+30] = byte(c.grow)
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(len(raw)))
	_, _ = r.Exec("INSERT IGNORE INTO taiwan_cain.charac_view (m_id) VALUES (?)", uid)
	_, err = r.Exec("UPDATE taiwan_cain.charac_view SET info=?,slot_effect_count=18,charac_slot_limit=18,hash_key='',charac_count=? WHERE m_id=?", blob, len(chars), uid)
	return err
}

func (r *SQLRepository) CopyTemplateDefaults(cid int) error {
	var src int
	if err := r.QueryRow("SELECT charac_no FROM taiwan_cain.charac_info WHERE charac_no<>? ORDER BY lev DESC,charac_no LIMIT 1", cid).Scan(&src); err != nil {
		return err
	}
	_, _ = r.Exec("UPDATE taiwan_cain.charac_info dst JOIN taiwan_cain.charac_info src SET dst.element_resist=src.element_resist,dst.spec_property=src.spec_property,dst.VIP=src.VIP,dst.create_time=src.create_time WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	_, _ = r.Exec("UPDATE taiwan_cain.charac_stat dst JOIN taiwan_cain.charac_stat src SET dst.tutorial_flag=src.tutorial_flag,dst.escalade_tutorial_flag=src.escalade_tutorial_flag,dst.open_flag=src.open_flag,dst.luck_point=src.luck_point WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	_, _ = r.Exec("UPDATE taiwan_cain_2nd.skill dst JOIN taiwan_cain_2nd.skill src SET dst.skill_slot=src.skill_slot,dst.skill_slot_2nd=src.skill_slot_2nd,dst.skill_command=src.skill_command,dst.script_version=src.script_version WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	return nil
}
