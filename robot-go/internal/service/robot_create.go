package service

import (
	"fmt"
	"strings"
	"time"
)

func (m *RobotManager) CreateRobots(req RobotCreateRequest) ([]RobotInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	_, finishOperation, err := m.beginTrackedStructuralOperation("create", fmt.Sprintf("count=%d", req.Count))
	if err != nil {
		return nil, err
	}
	var opErr error
	var created int
	defer func() {
		finishOperation(fmt.Sprintf("created=%d", created), opErr)
	}()

	m.mu.Lock()
	defer m.mu.Unlock()

	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 200 {
		req.Count = 200
	}
	rc := m.loadRobotConfig()
	maps := m.loadMapCatalog()
	if err := m.ensureSchema(); err != nil {
		opErr = err
		return nil, err
	}

	accountAutoInc, err := m.accountAutoIncrement()
	if err != nil {
		opErr = err
		return nil, err
	}
	uidList, err := m.availableRobotUIDs(req.Count, rc.RobotUIDStart, accountAutoInc)
	if err != nil {
		opErr = err
		return nil, err
	}
	nextCID, err := m.nextInt("SELECT charac_no FROM taiwan_cain.charac_info ORDER BY charac_no DESC LIMIT 1", 0)
	if err != nil {
		opErr = err
		return nil, err
	}
	nextCID++

	robots := make([]RobotInfo, 0, req.Count)
	usedNames := make(map[string]struct{}, req.Count)
	for i := 0; i < req.Count; i++ {
		info := RobotInfo{
			UID:     uidList[i],
			CID:     nextCID + i,
			Name:    m.robotName(uidList[i], usedNames, rc),
			Level:   m.randBetween(rc.LevelMin, rc.LevelMax),
			Job:     m.randomFrom(rc.Jobs),
			Grow:    m.randomFrom(rc.GrowTypes),
			Port:    m.cfg.RobotGamePort,
			Village: rc.SpawnFallbackVillage,
			Area:    rc.SpawnArea,
			X:       m.randBetween(rc.SpawnXMin, rc.SpawnXMax),
			Y:       m.randBetween(rc.SpawnYMin, rc.SpawnYMax),
		}
		if mp, ok := m.randomMap(maps, info.Level); ok {
			info.Village = mp.Village
			info.Area = mp.Area
			info.X = m.randBetween(mp.XMin, mp.XMax)
			info.Y = m.randBetween(mp.YMin, mp.YMax)
		}
		m.applyConfiguredLocation(&info, rc, maps)
		if err := m.createRobot(info, rc); err != nil {
			opErr = err
			return robots, err
		}
		robots = append(robots, info)
		created = len(robots)
	}
	return robots, nil
}

func (m *RobotManager) createRobot(info RobotInfo, rc robotRuntimeConfig) error {
	if err := m.ensureAccount(info.UID); err != nil {
		return err
	}
	dbName := robotNameForEncoding(info.Name, "utf8_cp1252")
	if _, err := m.db.Exec(
		"INSERT INTO taiwan_cain.charac_info (m_id,charac_no,charac_name,village,maxHP,maxMP,phy_attack,phy_defense,mag_attack,mag_defense,inven_weight,hp_regen,mp_regen,move_speed,attack_speed,cast_speed,hit_recovery,jump,charac_weight,max_fatigue,lev,job,grow_type) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		info.UID, info.CID, dbName, info.Village, "1800", "1400", "75", "75", "45", "45", "480000", "0", "500", "8500", "8500", "7000", "6000", "4300", "680000", "156", info.Level, info.Job, info.Grow,
	); err != nil {
		return fmt.Errorf("insert charac_info uid=%d cid=%d: %w", info.UID, info.CID, err)
	}
	if _, err := m.db.Exec("INSERT INTO taiwan_cain.charac_stat (charac_no,HP,exp,tutorial_flag,village) VALUES (?,?,?,?,?)", info.CID, "100", 0, "-1", info.Village); err != nil {
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
		{"INSERT IGNORE INTO taiwan_cain_2nd.inventory (charac_no,money,coin,inventory_capacity,inventory,equipslot) VALUES (?,?,?,?,?,?)", []interface{}{info.CID, rc.DefaultMoney, rc.DefaultCoin, rc.InventoryCapacity, buildCompressedZeros(249 * 61), buildCompressedZeros(12 * 61)}},
		{"INSERT IGNORE INTO taiwan_cain_2nd.skill (charac_no) VALUES (?)", []interface{}{info.CID}},
		{"INSERT IGNORE INTO taiwan_game_event.event_1306_account_reward (m_id,charac_no,occ_date) VALUES (?,?,NOW())", []interface{}{info.UID, info.CID}},
	}
	for _, q := range optional {
		_, _ = m.db.Exec(q.query, q.args...)
	}
	_ = m.copyTemplateDefaults(info.CID)
	if err := m.equipFromCatalog(info.CID, info.Level, info.Job, rc); err != nil {
		return err
	}
	if err := m.avatarFromCatalog(info.CID, info.Level, info.Job, rc); err != nil {
		return err
	}
	if err := m.populateStoreInventory(info, rc); err != nil {
		return err
	}
	if err := m.rebuildCharacView(info.UID); err != nil {
		return err
	}
	if err := m.upsertDummy(info); err != nil {
		return err
	}
	return m.registerRobot(info)
}

func (m *RobotManager) ensureAccount(uid int) error {
	now := time.Now().Unix()
	account := fmt.Sprintf("%d", uid)
	steps := []string{
		"CREATE TABLE IF NOT EXISTS d_taiwan.member_info_bot_backup LIKE d_taiwan.member_info",
		"DELETE FROM d_taiwan.member_join_info WHERE m_id=?",
		"DELETE FROM taiwan_login.member_join_info WHERE m_id=?",
	}
	for _, q := range steps {
		if strings.Contains(q, "?") {
			if _, err := m.db.Exec(q, uid); err != nil {
				return err
			}
		} else if _, err := m.db.Exec(q); err != nil {
			return err
		}
	}
	if err := m.insertIgnore("d_taiwan.accounts", map[string]interface{}{
		"UID": uid, "accountname": account, "password": "e10adc3949ba59abbe56e057f20f883e",
		"qq": "123456", "VIP": "", "ip": m.cfg.RobotInnerIP, "login_IP": "", "login_Mac": "",
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
		if err := m.insertIgnore(u.table, u.vals); err != nil {
			return err
		}
	}
	_, err := m.db.Exec("INSERT INTO taiwan_login.member_login (m_id,login_time,expire_time,last_play_time,login_ip,cleanpad_point,tutorial_skipable) VALUES (?,?,?,?,?,1,'1') ON DUPLICATE KEY UPDATE login_time=VALUES(login_time),expire_time=2147483647,last_play_time=VALUES(last_play_time),login_ip=VALUES(login_ip),cleanpad_point=1,tutorial_skipable='1'", uid, now, 2147483647, now, m.cfg.RobotInnerIP)
	if err != nil {
		return err
	}
	_, _ = m.db.Exec("INSERT IGNORE INTO taiwan_login.member_game_option VALUES (?,0x48000000789C63646064F85FCFCC90028408F0BF9E9181112C038023042210009AC0C9B,'','',0x10020000789C636018058319686115D5C62AAA83555417ABA81E56517D06003C02010C)", uid)
	_, _ = m.db.Exec("INSERT IGNORE INTO taiwan_login_play.member_key_option (m_id,key_type,key_option) VALUES (?,0,UNHEX(''))", uid)
	return nil
}

func (m *RobotManager) ensureSchema() error {
	stmts := []string{
		"CREATE DATABASE IF NOT EXISTS d_starsky DEFAULT CHARACTER SET gbk",
		"CREATE TABLE IF NOT EXISTS d_starsky.Dummylist (ID VARCHAR(32), YID VARCHAR(32), UID VARCHAR(32), port VARCHAR(16), curvill VARCHAR(32), curarea VARCHAR(32), curx VARCHAR(32), cury VARCHAR(32), CID VARCHAR(32), ip VARCHAR(64), function_type VARCHAR(8), discost VARCHAR(8), PRIMARY KEY (UID)) DEFAULT CHARSET=gbk",
		"CREATE TABLE IF NOT EXISTS d_starsky.v4_ai_user (uid VARCHAR(32) NOT NULL, msg_state VARCHAR(8) DEFAULT '0', move_state VARCHAR(8) DEFAULT '0', PRIMARY KEY (uid)) DEFAULT CHARSET=gbk",
		"CREATE TABLE IF NOT EXISTS d_starsky.Robot_stall (id INT NOT NULL AUTO_INCREMENT, Trade_item INT DEFAULT 0, price BIGINT DEFAULT 0, item_number INT DEFAULT 1, function_type INT DEFAULT 1, state INT DEFAULT 1, UID INT DEFAULT 0, PRIMARY KEY (id), KEY idx_robot_stall (function_type,state,UID)) DEFAULT CHARSET=gbk",
		"CREATE TABLE IF NOT EXISTS d_starsky.Robot_stall_config (id INT NOT NULL AUTO_INCREMENT, cfg_content VARCHAR(255) DEFAULT '', cfg_type INT DEFAULT 0, UID INT DEFAULT 0, function_type INT DEFAULT 1, state INT DEFAULT 1, PRIMARY KEY (id), KEY idx_robot_stall_cfg (cfg_type,function_type,state,UID)) DEFAULT CHARSET=gbk",
		"CREATE TABLE IF NOT EXISTS d_starsky.robot_registry (uid INT NOT NULL, cid INT NOT NULL, account VARCHAR(32) NOT NULL, charac_name VARCHAR(64) NOT NULL, created_at DATETIME NOT NULL, PRIMARY KEY (uid), KEY idx_robot_registry_cid (cid)) DEFAULT CHARSET=utf8",
		"INSERT IGNORE INTO d_starsky.Robot_stall_config (id,cfg_content,cfg_type,UID,function_type,state) VALUES (1,'bot-store',3,0,2,1)",
	}
	for _, stmt := range stmts {
		if _, err := m.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (m *RobotManager) upsertDummy(info RobotInfo) error {
	_, err := m.db.Exec("INSERT INTO d_starsky.Dummylist (ID,YID,UID,port,curvill,curarea,curx,cury,CID,ip,function_type,discost) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE port=VALUES(port),curvill=VALUES(curvill),curarea=VALUES(curarea),curx=VALUES(curx),cury=VALUES(cury),CID=VALUES(CID),ip=VALUES(ip)",
		info.UID, info.UID, info.UID, info.Port, info.Village, info.Area, info.X, info.Y, info.CID, m.cfg.RobotInnerIP, "0", "0")
	return err
}

func (m *RobotManager) registerRobot(info RobotInfo) error {
	account := fmt.Sprintf("%d", info.UID)
	_, err := m.db.Exec("INSERT INTO d_starsky.robot_registry (uid,cid,account,charac_name,created_at) VALUES (?,?,?,?,NOW()) ON DUPLICATE KEY UPDATE cid=VALUES(cid),account=VALUES(account),charac_name=VALUES(charac_name)",
		info.UID, info.CID, account, info.Name)
	return err
}
