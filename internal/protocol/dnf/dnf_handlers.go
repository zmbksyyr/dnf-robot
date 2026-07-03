package dnf

import (
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"robot/internal/foundation/crypto"
	"robot/internal/foundation/message"
	"strings"
	"time"
)

// ---- handler_list.go ----
func (dt *DnfTableDrive) handleList(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgListRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	_ = req

	respond := message.NewMsgListRespond()

	robotMap := task.GetRobotVoMap()
	for _, vo := range robotMap {
		vo.mu.Lock()

		result := message.NewMsgListResult()
		result.SetId(int(vo.UID))
		result.SetIp(vo.IP)
		result.SetPort(vo.Port)
		result.SetUid(int(vo.UID))
		result.SetCid(int(vo.CID))
		result.SetConncount(int(vo.ConnCount))
		result.SetMaxreconn(int(vo.MaxReConn))
		result.SetUserstate(message.MsgListUserState(vo.State))
		result.SetLasterror(message.MsgListUserError(vo.LastError))
		result.SetCurvill(int(vo.CurVillage))
		result.SetCurarea(int(vo.CurArea))
		result.SetCurx(int(vo.CurX))
		result.SetCury(int(vo.CurY))
		result.SetRobotType(vo.RobotTyp)

		if vo.State == StateRun {
			result.SetRuntime(int(uint32(time.Now().Unix()) - vo.RunStartTime))
		} else {
			result.SetRuntime(0)
		}

		vo.mu.Unlock()

		respond.AddUserstatus(result)
	}

	jsonStr, _ := respond.SerializeToString()
	return DnfTableTaskResult{Code: 200, Msg: jsonStr}
}

// ---- handler_logout.go ----
func (dt *DnfTableDrive) handleLogout(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgLogoutRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() {
			return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
		}

		task.AddMessage("MsgLogout", userInfo.Id())
	}

	return DnfTableTaskResult{Code: 200}
}

// ---- handler_move.go ----
func (dt *DnfTableDrive) handleMove(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgMoveRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasVillage() || !userInfo.HasArea() ||
			!userInfo.HasX() || !userInfo.HasY() {
			return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
		}

		if !robotIsRunning(task, userInfo.Id()) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}

		md := &moveInternalData{
			ID:       userInfo.Id(),
			Village:  uint8(userInfo.GetVillage()),
			Area:     uint8(userInfo.GetArea()),
			X:        uint16(userInfo.GetX()),
			Y:        uint16(userInfo.GetY()),
			MoveType: uint8(userInfo.TypeVal()),
			Speed:    uint16(userInfo.GetSpeed()),
		}
		task.AddMessage("MsgMove", md)
	}

	return DnfTableTaskResult{Msg: "ok", Code: 200}
}

func robotIsRunning(task *RobotDnfTask, uid int) bool {
	if task == nil {
		return false
	}
	vo := task.Find(uid)
	if vo == nil {
		return false
	}
	vo.mu.Lock()
	defer vo.mu.Unlock()
	return vo.State == StateRun
}

// ---- handler_online.go ----
var (
	rsaKey *rsa.PrivateKey
	dbPool *sql.DB
)

func SetRSAKey(key *rsa.PrivateKey) {
	rsaKey = key
}

func SetDBPool(db *sql.DB) {
	dbPool = db
}

func GetDBPool() *sql.DB {
	return dbPool
}

func (dt *DnfTableDrive) handleOnLine(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgOnLineRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "check err"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "check err"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)

		if !userInfo.HasId() || !userInfo.HasIp() || !userInfo.HasPort() || !userInfo.HasDelay() ||
			!userInfo.HasUid() || !userInfo.HasCid() || !userInfo.HasMaxreconn() ||
			!userInfo.HasRedelay() || !userInfo.HasBirthvill() || !userInfo.HasBirtharea() ||
			!userInfo.HasBirthx() || !userInfo.HasBirthy() || !userInfo.HasDisopen() || !userInfo.HasStoreopen() {
			return DnfTableTaskResult{Msg: "check err"}
		}

		if len(userInfo.Ip()) > 15 || userInfo.GetPort() <= 0 || userInfo.GetPort() >= 65536 ||
			userInfo.GetDelay() < 0 || userInfo.Maxreconn() < 0 || userInfo.Redelay() < 0 {
			return DnfTableTaskResult{Msg: "check err"}
		}

		if userInfo.Disopen() != 0 && !userInfo.HasDiscost() {
			return DnfTableTaskResult{Msg: "check err"}
		}

		if userInfo.Storeopen() != 0 && !userInfo.HasStoretitle() {
			return DnfTableTaskResult{Msg: "check err"}
		}
	}

	db := dbPool
	if db == nil {
		return DnfTableTaskResult{Msg: "no database connection"}
	}

	respond := message.NewMsgOnLineRespond()

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !repairLoginPrerequisites(db, userInfo.Uid(), userInfo.Ip()) {
			return DnfTableTaskResult{Msg: "repair login prerequisites failed"}
		}
	}
	if task.onlineBacklog()+req.UserinfosSize() > maxMessageQueueSize {
		return DnfTableTaskResult{Msg: "online queue full"}
	}
	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)

		loginInfo := UserLoginInfo{
			IP:        userInfo.Ip(),
			Port:      userInfo.GetPort(),
			Delay:     uint32(userInfo.GetDelay()),
			UID:       uint32(userInfo.Uid()),
			CID:       userInfo.Cid(),
			MaxReConn: uint32(userInfo.Maxreconn()),
			ReDelay:   uint32(userInfo.Redelay()),
			BirthPos: [4]uint32{
				uint32(userInfo.Birthvill()),
				uint32(userInfo.Birtharea()),
				uint32(userInfo.Birthx()),
				uint32(userInfo.Birthy()),
			},
		}

		if rsaKey != nil {
			tokenStr := generateLoginToken(userInfo.Uid(), rsaKey)
			if tokenStr == "" {
				var err error
				tokenStr, err = crypto.GetLoginKey(uint32(userInfo.Uid()), rsaKey)
				fmt.Printf("[TOKEN_ERR] openssl token failed; fallback GetLoginKey len=%d err=%v\n", len(tokenStr), err)
			}
			if tokenStr != "" {
				tokenBytes := []byte(tokenStr)
				copy(loginInfo.Token[:], tokenBytes)
				loginInfo.TokenSize = uint32(len(tokenBytes))
				if loginInfo.TokenSize > 512 {
					loginInfo.TokenSize = 512
				}
			}
		}

		vo := NewRobotVo(db)
		vo.Load(loginInfo)

		asyncTaskVec := make([]AsyncTask, 0)

		if userInfo.Disopen() != 0 {
			task2 := AsyncTask{
				Type: AsyncDisjoint,
				Cost: userInfo.Discost(),
			}
			asyncTaskVec = append(asyncTaskVec, task2)
		}

		if userInfo.Storeopen() != 0 {
			task3 := AsyncTask{
				Type:  AsyncPriStore,
				Title: userInfo.Storetitle(),
			}
			asyncTaskVec = append(asyncTaskVec, task3)
		}

		vo.AfterRunAsyncTaskVec = asyncTaskVec
		if !task.TryAddMessage("MsgOnLine", vo) {
			return DnfTableTaskResult{Msg: "online queue full"}
		}

		result := message.NewMsgOnLineResult()
		result.SetId(userInfo.Id())
		result.SetStatus(message.ONLINE_NO_ERROR)
		respond.AddUserstatus(result)
	}

	jsonStr, _ := respond.SerializeToString()
	return DnfTableTaskResult{Msg: jsonStr, Code: 200}
}

func repairLoginPrerequisites(db *sql.DB, uid int, loginIP string) bool {
	if uid <= 0 {
		return false
	}

	if !runOnlineRepairSQL(db, "CREATE TABLE IF NOT EXISTS d_taiwan.member_info_bot_backup LIKE d_taiwan.member_info", "create member_info_bot_backup") {
		return false
	}

	sqls := []struct {
		query string
		args  []interface{}
	}{
		{"INSERT IGNORE INTO d_taiwan.member_info_bot_backup (m_id,user_id) VALUES (?,?)", []interface{}{uid, fmt.Sprintf("%d", uid)}},
		{"UPDATE d_taiwan.member_info_bot_backup SET user_id=?,state=1,slot=8,hangame_flag=0 WHERE m_id=?", []interface{}{fmt.Sprintf("%d", uid), uid}},
		{"INSERT IGNORE INTO taiwan_login.allow_proxy_user (m_id) VALUES (?)", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.login_account_3 (m_id,m_channel_no) VALUES (?,'3011')", []interface{}{uid}},
		{"UPDATE taiwan_login.login_account_3 SET login_status=0 WHERE m_id=?", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.churn_member_info (m_id,play_info) VALUES (?,'000000000000000000000000000011')", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.member_game_option VALUES (?,0x48000000789C63646064F85FCF90028408F0BF9E9181112C0382CC50B117CC20F114A038023042210009AC0C9,'','',0x10020000789C636018058319686115D5C62AAA83555417ABA81E56517D06003C02010C)", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login_play.member_key_option (m_id,key_type,key_option) VALUES (?,0,UNHEX(''))", []interface{}{uid}},
	}

	for _, s := range sqls {
		if !runOnlineRepairSQL(db, s.query, "repair step", s.args...) {
			return false
		}
	}
	joinArgs := []interface{}{uid, time.Now().Year(), loginIP, loginIP}
	for _, table := range []string{"d_taiwan.member_join_info", "taiwan_login.member_join_info"} {
		query := "INSERT INTO " + table + " (m_id,reg_date,ip,contry_code,login_time,error_type,login_ip,game_use_history) VALUES (?,?,?,0,UNIX_TIMESTAMP(),0,?,1) ON DUPLICATE KEY UPDATE ip=VALUES(ip),login_time=VALUES(login_time),error_type=0,login_ip=VALUES(login_ip),game_use_history=1"
		if !runOnlineRepairSQLIfTableExists(db, table, query, "upsert member_join_info", joinArgs...) {
			return false
		}
	}
	for _, table := range []string{"d_taiwan.member_security_grade", "d_taiwan_secu.member_security_grade"} {
		query := "INSERT IGNORE INTO " + table + " (m_id) VALUES (?)"
		if !runOnlineRepairSQLIfTableExists(db, table, query, "upsert member_security_grade", uid) {
			return false
		}
	}
	if !runOnlineRepairSQLIfTableExists(db, "d_taiwan.member_punish_info", "DELETE FROM d_taiwan.member_punish_info WHERE m_id=? AND punish_type=11", "clear trade punish", uid) {
		return false
	}
	if !repairOnlineRobotExpBounds(db, uid) {
		return false
	}

	stmtSQL := "INSERT INTO taiwan_login.member_login (m_id,login_time,expire_time,last_play_time,login_ip,cleanpad_point,tutorial_skipable) VALUES (?,UNIX_TIMESTAMP(),2147483647,UNIX_TIMESTAMP(),?,1,'1') ON DUPLICATE KEY UPDATE login_time=UNIX_TIMESTAMP(),expire_time=2147483647,last_play_time=UNIX_TIMESTAMP(),login_ip=VALUES(login_ip),cleanpad_point=1,tutorial_skipable='1'"
	if _, err := db.Exec(stmtSQL, uid, loginIP); err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: upsert member_login: %v\n", err)
		return false
	}

	return true
}

func repairOnlineRobotExpBounds(db *sql.DB, uid int) bool {
	if !onlineTableExists(db, "d_starsky.robot_registry") {
		return true
	}
	refRows := 0
	if onlineTableExists(db, "taiwan_cain.exp_level_ref") {
		_ = db.QueryRow("SELECT COUNT(*) FROM taiwan_cain.exp_level_ref").Scan(&refRows)
	}
	if refRows > 0 {
		infoSQL := `UPDATE taiwan_cain.charac_info c
			JOIN d_starsky.robot_registry r ON r.cid=c.charac_no
			JOIN taiwan_cain.exp_level_ref e ON e.lev=c.lev
			LEFT JOIN taiwan_cain.exp_level_ref n ON n.lev=c.lev+1
			SET c.exp=e.exp
			WHERE r.uid=? AND (c.exp<e.exp OR (n.exp IS NOT NULL AND c.exp>=n.exp))`
		if !runOnlineRepairSQL(db, infoSQL, "repair charac_info exp", uid) {
			return false
		}
		statSQL := `UPDATE taiwan_cain.charac_stat s
			JOIN taiwan_cain.charac_info c ON c.charac_no=s.charac_no
			JOIN d_starsky.robot_registry r ON r.cid=c.charac_no
			JOIN taiwan_cain.exp_level_ref e ON e.lev=c.lev
			LEFT JOIN taiwan_cain.exp_level_ref n ON n.lev=c.lev+1
			SET s.exp=e.exp
			WHERE r.uid=? AND (s.exp<e.exp OR (n.exp IS NOT NULL AND s.exp>=n.exp))`
		return runOnlineRepairSQL(db, statSQL, "repair charac_stat exp", uid)
	}
	var lev, infoExp, statExp int
	err := db.QueryRow(`SELECT IFNULL(c.lev,0),IFNULL(c.exp,0),IFNULL(s.exp,0)
		FROM d_starsky.robot_registry r
		JOIN taiwan_cain.charac_info c ON c.charac_no=r.cid
		LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=r.cid
		WHERE r.uid=? LIMIT 1`, uid).Scan(&lev, &infoExp, &statExp)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: read robot exp: %v\n", err)
		return false
	}
	minExp, ok := onlineRobotLevelMinExp(lev)
	if !ok || (infoExp >= minExp && statExp >= minExp) {
		return true
	}
	if !runOnlineRepairSQL(db, "UPDATE taiwan_cain.charac_info c JOIN d_starsky.robot_registry r ON r.cid=c.charac_no SET c.exp=? WHERE r.uid=? AND c.exp<?", "repair charac_info exp fallback", minExp, uid, minExp) {
		return false
	}
	return runOnlineRepairSQL(db, "UPDATE taiwan_cain.charac_stat s JOIN d_starsky.robot_registry r ON r.cid=s.charac_no SET s.exp=? WHERE r.uid=? AND s.exp<?", "repair charac_stat exp fallback", minExp, uid, minExp)
}

func onlineTableExists(db *sql.DB, table string) bool {
	schema, name, ok := strings.Cut(table, ".")
	if !ok {
		return false
	}
	var tableName sql.NullString
	err := db.QueryRow("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=? LIMIT 1", schema, name).Scan(&tableName)
	return err == nil && tableName.Valid
}

func onlineRobotLevelMinExp(level int) (int, bool) {
	if level < 1 || level >= len(onlineRobotLevelMinExpTable) {
		return 0, false
	}
	return onlineRobotLevelMinExpTable[level], true
}

var onlineRobotLevelMinExpTable = []int{
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

func runOnlineRepairSQLIfTableExists(db *sql.DB, table string, query string, step string, args ...interface{}) bool {
	schema, name, ok := strings.Cut(table, ".")
	if !ok {
		fmt.Printf("MsgOnLine preflight sql failed: invalid optional table %s\n", table)
		return false
	}
	var tableName sql.NullString
	err := db.QueryRow("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=? LIMIT 1", schema, name).Scan(&tableName)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: check optional %s: %v\n", table, err)
		return false
	}
	if !tableName.Valid {
		return true
	}
	return runOnlineRepairSQL(db, query, step, args...)
}

func deleteOnlineRepairRowIfTableExists(db *sql.DB, table, col string, uid int) bool {
	if db == nil {
		fmt.Printf("MsgOnLine preflight sql failed: delete optional %s (no db)\n", table)
		return false
	}
	schema, name, ok := strings.Cut(table, ".")
	if !ok {
		fmt.Printf("MsgOnLine preflight sql failed: invalid optional table %s\n", table)
		return false
	}
	var tableName sql.NullString
	err := db.QueryRow("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=? LIMIT 1", schema, name).Scan(&tableName)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: check optional %s: %v\n", table, err)
		return false
	}
	if !tableName.Valid {
		return true
	}
	quoted := "`" + strings.ReplaceAll(schema, "`", "``") + "`.`" + strings.ReplaceAll(name, "`", "``") + "`"
	if _, err := db.Exec("DELETE FROM "+quoted+" WHERE `"+col+"`=?", uid); err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: delete optional %s: %v\n", table, err)
		return false
	}
	return true
}

func runOnlineRepairSQL(db *sql.DB, query string, step string, args ...interface{}) bool {
	if db == nil {
		fmt.Printf("MsgOnLine preflight sql failed: %s (no db)\n", step)
		return false
	}
	if _, err := db.Exec(query, args...); err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: %s: %v\n", step, err)
		return false
	}
	return true
}

// [C++->Go] Token format from report.md fix:
// bytes 0-3: swapByte32(uid)
// bytes 4-35: UID decimal string plus NUL padding.
// bytes 36-39: CURRENT timestamp field.
// bytes 40-45: 0x01 0x04 0x03 0x03 0x01 0x01
func generateLoginToken(uid int, key *rsa.PrivateKey) string {
	if key == nil {
		return ""
	}
	now := uint32(time.Now().Unix())
	token := make([]byte, 46)
	binary.BigEndian.PutUint32(token[0:4], uint32(uid))
	uidStr := fmt.Sprintf("%d", uid)
	copy(token[4:], []byte(uidStr))
	binary.BigEndian.PutUint32(token[36:40], now)
	token[40] = 0x01
	token[41] = 0x04
	token[42] = 0x03
	token[43] = 0x03
	token[44] = 0x01
	token[45] = 0x01
	sig, err := rsa.SignPKCS1v15(nil, key, 0, token)
	if err != nil {
		fmt.Printf("[TOKEN_ERR] RSA sign failed: %v\n", err)
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// ---- handler_public.go ----
func (dt *DnfTableDrive) handlePublicMsg(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgPublicMsgRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasMsg() || len(userInfo.Msg()) == 0 {
			return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
		}

		if !robotIsRunning(task, userInfo.Id()) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}

		md := &publicMsgInternalData{
			ID:   userInfo.Id(),
			Msg:  userInfo.Msg(),
			Type: userInfo.TypeValue(),
		}
		task.AddMessage("MsgPublicMsg", md)
	}

	return DnfTableTaskResult{Code: 200}
}

// ---- handler_remove.go ----
func (dt *DnfTableDrive) handleRemove(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgRemoveRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() {
			return DnfTableTaskResult{Msg: "閺嶏繝鐛欐径杈Е"}
		}

		task.AddMessage("MsgLogout", userInfo.Id())
	}

	return DnfTableTaskResult{Code: 200}
}
