package repository

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"

	catalogcap "robot/internal/capability/catalog"
	equipcap "robot/internal/capability/equipment"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robottemplate "robot/internal/capability/robottemplate"
	storecap "robot/internal/capability/store"
	"robot/internal/foundation/charset"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

const (
	equipmentRecordSize = 61
	inventorySlotCount  = 249
	equipslotSlotCount  = 12
)

type characterInitializer struct {
	table    string
	query    string
	args     []interface{}
	required bool
}

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
	r.invalidateTableExists("d_taiwan.member_info_bot_backup")
	if err := r.InsertIgnore("d_taiwan.accounts", map[string]interface{}{
		"UID": uid, "accountname": account, "password": "e10adc3949ba59abbe56e057f20f883e",
		"qq": "123456", "VIP": "", "ip": innerIP, "login_IP": "", "login_Mac": "",
		"seal_IP": 0, "seal_MAC": 0, "seal_accountname": 0,
	}); err != nil {
		return err
	}
	var existingAccount string
	if err := r.QueryRow("SELECT accountname FROM d_taiwan.accounts WHERE UID=? LIMIT 1", uid).Scan(&existingAccount); err != nil {
		return err
	}
	if existingAccount != account {
		return fmt.Errorf("robot uid %d is occupied by account %q", uid, existingAccount)
	}
	if _, err := r.ClearTradePunish(uid); err != nil {
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
	if err := r.execIfTableExists(
		"taiwan_login.member_game_option",
		"INSERT IGNORE INTO taiwan_login.member_game_option VALUES (?,0x48000000789C63646064F85FCFCC90028408F0BF9E9181112C038023042210009AC0C9B,'','',0x10020000789C636018058319686115D5C62AAA83555417ABA81E56517D06003C02010C)",
		uid,
	); err != nil {
		return fmt.Errorf("initialize member_game_option uid=%d: %w", uid, err)
	}
	if err := r.execIfTableExists(
		"taiwan_login_play.member_key_option",
		"INSERT IGNORE INTO taiwan_login_play.member_key_option (m_id,key_type,key_option) VALUES (?,0,UNHEX(''))",
		uid,
	); err != nil {
		return fmt.Errorf("initialize member_key_option uid=%d: %w", uid, err)
	}
	return nil
}

func (r *SQLRepository) execIfTableExists(table, query string, args ...interface{}) error {
	exists, err := r.TableExists(table)
	if err != nil || !exists {
		return err
	}
	_, err = r.Exec(query, args...)
	return err
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
		return 0, err
	}
	return rows, nil
}

func (r *SQLRepository) CreateBaseCharacter(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	exp, ok := catalogcap.LevelMinExp(info.Level)
	if !ok {
		return fmt.Errorf("PVF level experience unavailable for level %d", info.Level)
	}
	dbName := robottemplate.NameForEncoding(info.Name, "utf8_cp1252")
	columns, err := r.TableColumns("taiwan_cain.charac_info")
	if err != nil {
		return fmt.Errorf("read charac_info columns: %w", err)
	}
	infoQuery, infoArgs := createCharacterInfoInsert(info, exp, dbName, columns)
	statQuery, statArgs := createCharacterStatInsert(info.CID, exp, info.Village)
	skillSlots, err := maxRobotSkillSlots(info.Job, info.Grow)
	if err != nil {
		return err
	}
	initialInventory := make([]byte, inventorySlotCount*equipmentRecordSize)
	if _, err := storecap.EnsureDungeonSupplies(initialInventory); err != nil {
		return err
	}
	initializers := []characterInitializer{
		{table: "taiwan_cain.charac_kill_monster_info", query: "INSERT IGNORE INTO taiwan_cain.charac_kill_monster_info (charac_no) VALUES (?)", args: []interface{}{info.CID}},
		{table: "taiwan_cain.charac_link_message", query: "INSERT IGNORE INTO taiwan_cain.charac_link_message (m_id) VALUES (?)", args: []interface{}{info.UID}},
		{table: "taiwan_cain.charac_npc", query: "INSERT IGNORE INTO taiwan_cain.charac_npc (charac_no) VALUES (?)", args: []interface{}{info.CID}},
		{table: "taiwan_cain.member_dungeon", query: "INSERT IGNORE INTO taiwan_cain.member_dungeon (m_id) VALUES (?)", args: []interface{}{info.UID}},
		{table: "taiwan_cain.new_charac_quest", query: "INSERT IGNORE INTO taiwan_cain.new_charac_quest (charac_no,play_1) VALUES (?,?)", args: []interface{}{info.CID, "1016"}},
		{table: "taiwan_cain.pvp_result", query: "INSERT IGNORE INTO taiwan_cain.pvp_result (charac_no) VALUES (?)", args: []interface{}{info.CID}},
		{table: "taiwan_cain_2nd.charac_inven_expand", query: "INSERT IGNORE INTO taiwan_cain_2nd.charac_inven_expand (charac_no) VALUES (?)", args: []interface{}{info.CID}},
		{table: "taiwan_cain_2nd.inventory", query: "INSERT IGNORE INTO taiwan_cain_2nd.inventory (charac_no,money,coin,inventory_capacity,inventory,equipslot) VALUES (?,?,?,?,?,?)", args: []interface{}{info.CID, rc.DefaultMoney, rc.DefaultCoin, rc.InventoryCapacity, equipcap.CompressRaw(initialInventory), equipcap.CompressedZeros(equipslotSlotCount * equipmentRecordSize)}, required: true},
		{table: "taiwan_cain_2nd.skill", query: "INSERT INTO taiwan_cain_2nd.skill (charac_no,skill_slot,skill_slot_2nd,skill_command,script_version) VALUES (?,?,?,?,3)", args: []interface{}{info.CID, skillSlots, skillSlots, equipcap.CompressedZeros(200)}, required: true},
		{table: "taiwan_game_event.event_1306_account_reward", query: "INSERT IGNORE INTO taiwan_game_event.event_1306_account_reward (m_id,charac_no,occ_date) VALUES (?,?,NOW())", args: []interface{}{info.UID, info.CID}},
	}
	requiredInitializers := make([]characterInitializer, 0, 2)
	optionalInitializers := make([]characterInitializer, 0, len(initializers)-2)
	for _, initializer := range initializers {
		exists, err := r.TableExists(initializer.table)
		if err != nil {
			return fmt.Errorf("check character table %s: %w", initializer.table, err)
		}
		if !exists {
			if initializer.required {
				return fmt.Errorf("required character table %s does not exist", initializer.table)
			}
			continue
		}
		if initializer.required {
			requiredInitializers = append(requiredInitializers, initializer)
		} else {
			optionalInitializers = append(optionalInitializers, initializer)
		}
	}

	tx, err := r.Begin()
	if err != nil {
		return fmt.Errorf("begin character creation uid=%d cid=%d: %w", info.UID, info.CID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(infoQuery, infoArgs...); err != nil {
		return fmt.Errorf("insert charac_info uid=%d cid=%d: %w", info.UID, info.CID, err)
	}
	if _, err := tx.Exec(statQuery, statArgs...); err != nil {
		return fmt.Errorf("insert charac_stat cid=%d: %w", info.CID, err)
	}
	for _, initializer := range requiredInitializers {
		if _, err := tx.Exec(initializer.query, initializer.args...); err != nil {
			return fmt.Errorf("initialize character table %s uid=%d cid=%d: %w", initializer.table, info.UID, info.CID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit character creation uid=%d cid=%d: %w", info.UID, info.CID, err)
	}
	applyOptionalCharacterInitializers(optionalInitializers, func(query string, args ...interface{}) error {
		_, err := r.Exec(query, args...)
		return err
	}, func(table string, err error) {
		foundationlog.Robotf("[RobotCreate] optional initializer skipped table=%s uid=%d cid=%d err=%v\n", table, info.UID, info.CID, err)
	})
	return nil
}

func applyOptionalCharacterInitializers(initializers []characterInitializer, execute func(string, ...interface{}) error, report func(string, error)) {
	for _, initializer := range initializers {
		if err := execute(initializer.query, initializer.args...); err != nil && report != nil {
			report(initializer.table, err)
		}
	}
}

func createCharacterInfoInsert(info robotcap.Info, exp int, dbName interface{}, columns map[string]bool) (string, []interface{}) {
	names := []string{
		"m_id", "charac_no", "charac_name", "village", "maxHP", "maxMP", "phy_attack", "phy_defense",
		"mag_attack", "mag_defense", "inven_weight", "hp_regen", "mp_regen", "move_speed", "attack_speed",
		"cast_speed", "hit_recovery", "jump", "charac_weight", "max_fatigue", "lev", "exp", "job", "grow_type",
	}
	args := []interface{}{
		info.UID, info.CID, dbName, info.Village, "1800", "1400", "75", "75", "45", "45", "480000", "0",
		"500", "8500", "8500", "7000", "6000", "4300", "680000", "156", info.Level, exp, info.Job, info.Grow,
	}
	optional := []struct {
		name  string
		value interface{}
	}{
		{"element_resist", make([]byte, 8)},
		{"spec_property", make([]byte, 34)},
		{"VIP", ""},
		{"create_time", time.Now()},
	}
	for _, field := range optional {
		if columns[field.name] {
			names = append(names, field.name)
			args = append(args, field.value)
		}
	}
	holders := make([]string, len(names))
	for i := range holders {
		holders[i] = "?"
	}
	return "INSERT INTO taiwan_cain.charac_info (`" + strings.Join(names, "`,`") + "`) VALUES (" + strings.Join(holders, ",") + ")", args
}

func createCharacterStatInsert(cid, exp, village int) (string, []interface{}) {
	const query = "INSERT INTO taiwan_cain.charac_stat (charac_no,HP,exp,tutorial_flag,village,village_prev) VALUES (?,?,?,?,?,?)"
	return query, []interface{}{cid, "100", exp, "-1", village, village}
}

func (r *SQLRepository) SaveEquipmentSlots(cid int, raw []byte) error {
	_, err := r.Exec("UPDATE taiwan_cain_2nd.inventory SET equipslot=? WHERE charac_no=?", equipcap.CompressRaw(raw), cid)
	return err
}

func (r *SQLRepository) ReplaceAvatarItems(cid int, selected map[int]shared.EquipmentCatalogItem) error {
	tx, err := r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM taiwan_cain_2nd.user_items WHERE charac_no=? AND slot<=9", cid); err != nil {
		return err
	}
	slots := make([]int, 0, len(selected))
	for slot := range selected {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	for _, slot := range slots {
		item := selected[slot]
		if _, err := tx.Exec("INSERT INTO taiwan_cain_2nd.user_items (charac_no,slot,it_id,expire_date,reg_date,obtain_from,hidden_option,jewel_socket) VALUES (?,?,?,'9999-12-31 23:59:59',NOW(),0,1,UNHEX(''))", cid, slot, item.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplacePetItems persists one creature and a partial set of creature
// artifacts. Creature instances live in creature_items. The active creature
// and equipped artifacts live in records 98-101 of the creature blob.
func (r *SQLRepository) ReplacePetItems(cid int, pet shared.EquipmentCatalogItem, artifacts map[int]shared.EquipmentCatalogItem) error {
	if cid <= 0 {
		return fmt.Errorf("invalid pet character id %d", cid)
	}
	if pet.ID <= 0 {
		return fmt.Errorf("invalid pet item id %d for cid=%d", pet.ID, cid)
	}
	creatureTable, err := r.TableExists("taiwan_cain_2nd.creature_items")
	if err != nil {
		return err
	}
	if !creatureTable {
		return fmt.Errorf("taiwan_cain_2nd.creature_items is required when pet creation is enabled")
	}
	creatureColumns, err := r.TableColumns("taiwan_cain_2nd.creature_items")
	if err != nil {
		return err
	}
	inventoryColumns, err := r.TableColumns("taiwan_cain_2nd.inventory")
	if err != nil {
		return err
	}
	if !inventoryColumns["creature"] {
		return fmt.Errorf("taiwan_cain_2nd.inventory has no creature column")
	}
	tx, err := r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM taiwan_cain_2nd.creature_items WHERE charac_no=?", cid); err != nil {
		return err
	}
	insertQuery, insertArgs, err := petCreatureItemInsert(cid, pet.ID, creatureColumns)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(insertQuery, insertArgs...); err != nil {
		return err
	}
	query, args := petInventoryUpdate(cid, pet, artifacts, inventoryColumns["creature_flag"])
	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func petCreatureItemInsert(cid, petID int, columns map[string]bool) (string, []interface{}, error) {
	const prefix = "INSERT INTO taiwan_cain_2nd.creature_items "
	switch {
	case tableHasColumns(columns, "no_charge", "stat", "item_lock_key", "ipg_agency_no", "expire_date", "delete_date"):
		return prefix + "(charac_no,slot,it_id,reg_date,name,stomach,exp,endurance,creature_type,no_charge,stat,item_lock_key,ipg_agency_no,expire_date,delete_date) VALUES (?,238,?,NOW(),'',100,0,0,1,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')", []interface{}{cid, petID}, nil
	case tableHasColumns(columns, "creature_level", "item_lock", "delete_flag", "skills", "expire_time", "item_creature_expire_time"):
		return prefix + "(charac_no,slot,it_id,reg_date,name,stomach,exp,endurance,creature_type,creature_level,item_lock,delete_flag,skills,expire_time,item_creature_expire_time) VALUES (?,238,?,NOW(),'',100,0,0,1,0,0,0,'','9999-12-31 23:59:59','9999-12-31 23:59:59')", []interface{}{cid, petID}, nil
	default:
		return "", nil, fmt.Errorf("taiwan_cain_2nd.creature_items has no supported pet schema")
	}
}

func tableHasColumns(columns map[string]bool, names ...string) bool {
	for _, name := range names {
		if !columns[name] {
			return false
		}
	}
	return true
}

func petInventoryUpdate(cid int, pet shared.EquipmentCatalogItem, artifacts map[int]shared.EquipmentCatalogItem, creatureFlag bool) (string, []interface{}) {
	sets := []string{"creature=?"}
	args := []interface{}{equipcap.CompressedCreatureLoadout(pet.ID, artifacts)}
	if creatureFlag {
		sets = append(sets, "creature_flag=1")
	}
	args = append(args, cid)
	return "UPDATE taiwan_cain_2nd.inventory SET " + strings.Join(sets, ",") + " WHERE charac_no=?", args
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
	return rebuildCharacView(r, uid)
}

type characViewDB interface {
	Query(string, ...interface{}) (*sql.Rows, error)
	Exec(string, ...interface{}) (sql.Result, error)
}

func rebuildCharacView(db characViewDB, uid int) error {
	rows, err := db.Query("SELECT charac_no,charac_name,lev,job,grow_type FROM taiwan_cain.charac_info WHERE m_id=? AND delete_flag=0 ORDER BY charac_no", uid)
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
	if err := rows.Err(); err != nil {
		return err
	}
	if len(chars) > characterViewSlots {
		return fmt.Errorf("account uid=%d has %d characters, view supports %d", uid, len(chars), characterViewSlots)
	}
	raw := make([]byte, characterViewSlots*characterViewSlotBytes)
	for slot, c := range chars {
		off := slot * characterViewSlotBytes
		binary.LittleEndian.PutUint32(raw[off:off+4], uint32(c.cid))
		name := charset.Windows1252StringBytes(c.name)
		if len(name) > 20 {
			name = name[:20]
		}
		copy(raw[off+4:], name)
		raw[off+24] = byte(c.job)
		raw[off+25] = byte(c.grow)
		binary.LittleEndian.PutUint16(raw[off+26:], uint16(c.lev))
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(len(raw)))
	if _, err := db.Exec("INSERT IGNORE INTO taiwan_cain.charac_view (m_id) VALUES (?)", uid); err != nil {
		return err
	}
	_, err = db.Exec("UPDATE taiwan_cain.charac_view SET info=?,slot_effect_count=18,charac_slot_limit=18,hash_key='',charac_count=? WHERE m_id=?", blob, len(chars), uid)
	return err
}

func (r *SQLRepository) CopyTemplateDefaults(cid int) error {
	var src int
	if err := r.QueryRow("SELECT charac_no FROM taiwan_cain.charac_info WHERE charac_no<>? ORDER BY lev DESC,charac_no LIMIT 1", cid).Scan(&src); err != nil {
		return err
	}
	queries := []string{
		"UPDATE taiwan_cain.charac_info dst JOIN taiwan_cain.charac_info src SET dst.element_resist=src.element_resist,dst.spec_property=src.spec_property,dst.create_time=src.create_time WHERE dst.charac_no=? AND src.charac_no=?",
		"UPDATE taiwan_cain.charac_stat dst JOIN taiwan_cain.charac_stat src SET dst.tutorial_flag=src.tutorial_flag,dst.escalade_tutorial_flag=src.escalade_tutorial_flag,dst.open_flag=src.open_flag,dst.luck_point=src.luck_point WHERE dst.charac_no=? AND src.charac_no=?",
	}
	var vipColumn int
	if err := r.QueryRow("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='taiwan_cain' AND TABLE_NAME='charac_info' AND COLUMN_NAME='VIP'").Scan(&vipColumn); err != nil {
		return err
	}
	if vipColumn > 0 {
		queries[0] = strings.Replace(queries[0], "dst.create_time=", "dst.VIP=src.VIP,dst.create_time=", 1)
	}
	for _, query := range queries {
		if _, err := r.Exec(query, cid, src); err != nil {
			return err
		}
	}
	return nil
}
