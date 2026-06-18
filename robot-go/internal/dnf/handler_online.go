package dnf

import (
	"robot/pkg/crypto"

	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"robot/pkg/message"
)

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
		task.AddMessage("MsgOnLine", vo)

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
		{"DELETE FROM d_taiwan.member_join_info WHERE m_id=?", []interface{}{uid}},
		{"DELETE FROM taiwan_login.member_join_info WHERE m_id=?", []interface{}{uid}},
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

	stmtSQL := "INSERT INTO taiwan_login.member_login (m_id,login_time,expire_time,last_play_time,login_ip,cleanpad_point,tutorial_skipable) VALUES (?,UNIX_TIMESTAMP(),2147483647,UNIX_TIMESTAMP(),?,1,'1') ON DUPLICATE KEY UPDATE login_time=UNIX_TIMESTAMP(),expire_time=2147483647,last_play_time=UNIX_TIMESTAMP(),login_ip=VALUES(login_ip),cleanpad_point=1,tutorial_skipable='1'"
	if _, err := db.Exec(stmtSQL, uid, loginIP); err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: upsert member_login: %v\n", err)
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
// bytes 4-35: UID decimal string + \\0 padding (report: "UID的ASCII字符串及\\0填充")
// bytes 36-39: CURRENT timestamp (report: "由网关内部按当前时间生成")
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
