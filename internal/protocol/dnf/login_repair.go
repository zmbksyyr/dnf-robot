package dnf

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"robot/internal/shared"
)

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
	minExp, ok := shared.LevelMinExp(lev)
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
