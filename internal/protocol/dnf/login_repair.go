package dnf

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"robot/internal/foundation/lockhub"
)

type loginStaticRepairKey struct {
	db  *sql.DB
	uid int
}

type loginStaticRepairEntry struct {
	done      chan struct{}
	ok        bool
	expiresAt time.Time
}

type loginStaticRepairCache struct {
	access  lockhub.Locker
	entries map[loginStaticRepairKey]*loginStaticRepairEntry
	ttl     time.Duration
	now     func() time.Time
}

const (
	loginStaticRepairTTL = time.Hour
	loginRepairTimeout   = 5 * time.Second
)

func (c *loginStaticRepairCache) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *loginStaticRepairCache) successTTL() time.Duration {
	if c.ttl > 0 {
		return c.ttl
	}
	return loginStaticRepairTTL
}

func (c *loginStaticRepairCache) ensure(ctx context.Context, db *sql.DB, uid int, repair func(context.Context) bool) bool {
	if db == nil || uid <= 0 || repair == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := loginStaticRepairKey{db: db, uid: uid}
	c.access.Lock()
	if entry := c.entries[key]; entry != nil {
		c.access.Unlock()
		select {
		case <-entry.done:
		case <-ctx.Done():
			return false
		}
		if !entry.ok || c.currentTime().Before(entry.expiresAt) {
			return entry.ok
		}
		c.access.Lock()
		if c.entries[key] == entry {
			delete(c.entries, key)
		}
		c.access.Unlock()
		return c.ensure(ctx, db, uid, repair)
	}
	if c.entries == nil {
		c.entries = make(map[loginStaticRepairKey]*loginStaticRepairEntry)
	}
	entry := &loginStaticRepairEntry{done: make(chan struct{})}
	c.entries[key] = entry
	c.access.Unlock()

	ok := repair(ctx)
	c.access.Lock()
	entry.ok = ok
	if ok {
		entry.expiresAt = c.currentTime().Add(c.successTTL())
	}
	if !ok && c.entries[key] == entry {
		delete(c.entries, key)
	}
	close(entry.done)
	c.access.Unlock()
	return ok
}

func (c *loginStaticRepairCache) invalidateUIDs(uids []int) {
	if len(uids) == 0 {
		return
	}
	invalid := make(map[int]struct{}, len(uids))
	for _, uid := range uids {
		if uid > 0 {
			invalid[uid] = struct{}{}
		}
	}
	if len(invalid) == 0 {
		return
	}
	c.access.Lock()
	for key := range c.entries {
		if _, ok := invalid[key.uid]; ok {
			delete(c.entries, key)
		}
	}
	c.access.Unlock()
}

var loginStaticRepairs loginStaticRepairCache

func InvalidateLoginRepairs(uids []int) {
	loginStaticRepairs.invalidateUIDs(uids)
}

func repairLoginPrerequisites(db *sql.DB, uid int, loginIP string) bool {
	if db == nil || uid <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginRepairTimeout)
	defer cancel()

	capabilities, err := loginRepairCapabilitiesFor(ctx, db)
	if err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: inspect login repair schema: %v\n", err)
		return false
	}
	if !loginStaticRepairs.ensure(ctx, db, uid, func(ctx context.Context) bool {
		return repairStaticLoginPrerequisites(ctx, db, uid, capabilities)
	}) {
		return false
	}
	return refreshLoginSession(ctx, db, uid, loginIP, capabilities)
}

func repairStaticLoginPrerequisites(ctx context.Context, db *sql.DB, uid int, capabilities loginRepairCapabilities) bool {
	sqls := []struct {
		query string
		args  []interface{}
	}{
		{"INSERT IGNORE INTO d_taiwan.member_info_bot_backup (m_id,user_id) VALUES (?,?)", []interface{}{uid, fmt.Sprintf("%d", uid)}},
		{"UPDATE d_taiwan.member_info_bot_backup SET user_id=?,state=1,slot=8,hangame_flag=0 WHERE m_id=?", []interface{}{fmt.Sprintf("%d", uid), uid}},
		{"INSERT IGNORE INTO taiwan_login.allow_proxy_user (m_id) VALUES (?)", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.login_account_3 (m_id,m_channel_no) VALUES (?,'3011')", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.churn_member_info (m_id,play_info) VALUES (?,'000000000000000000000000000011')", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login.member_game_option VALUES (?,0x48000000789C63646064F85FCF90028408F0BF9E9181112C0382CC50B117CC20F114A038023042210009AC0C9,'','',0x10020000789C636018058319686115D5C62AAA83555417ABA81E56517D06003C02010C)", []interface{}{uid}},
		{"INSERT IGNORE INTO taiwan_login_play.member_key_option (m_id,key_type,key_option) VALUES (?,0,UNHEX(''))", []interface{}{uid}},
	}

	for _, s := range sqls {
		if !runOnlineRepairSQL(ctx, db, s.query, "repair step", s.args...) {
			return false
		}
	}
	for _, table := range capabilities.memberSecurityGradeTables() {
		query := "INSERT IGNORE INTO " + table + " (m_id) VALUES (?)"
		if !runOnlineRepairSQL(ctx, db, query, "upsert member_security_grade", uid) {
			return false
		}
	}
	return true
}

func refreshLoginSession(ctx context.Context, db *sql.DB, uid int, loginIP string, capabilities loginRepairCapabilities) bool {
	run := func(query string, step string, args ...interface{}) bool {
		return runOnlineRepairSQL(ctx, db, query, step, args...)
	}
	return refreshLoginSessionWith(uid, loginIP, capabilities, run)
}

type loginRepairExec func(query string, step string, args ...interface{}) bool

func refreshLoginSessionWith(uid int, loginIP string, capabilities loginRepairCapabilities, run loginRepairExec) bool {
	if !run("UPDATE taiwan_login.login_account_3 SET login_status=0 WHERE m_id=?", "reset login status", uid) {
		return false
	}
	joinArgs := []interface{}{uid, time.Now().Year(), loginIP, loginIP}
	for _, table := range capabilities.memberJoinInfoTables() {
		query := "INSERT INTO " + table + " (m_id,reg_date,ip,contry_code,login_time,error_type,login_ip,game_use_history) VALUES (?,?,?,0,UNIX_TIMESTAMP(),0,?,1) ON DUPLICATE KEY UPDATE ip=VALUES(ip),login_time=VALUES(login_time),error_type=0,login_ip=VALUES(login_ip),game_use_history=1"
		if !run(query, "upsert member_join_info", joinArgs...) {
			return false
		}
	}
	if capabilities.memberPunishInfo && !run("DELETE FROM d_taiwan.member_punish_info WHERE m_id=? AND punish_type=11", "clear trade punish", uid) {
		return false
	}
	stmtSQL := "INSERT INTO taiwan_login.member_login (m_id,login_time,expire_time,last_play_time,login_ip,cleanpad_point,tutorial_skipable) VALUES (?,UNIX_TIMESTAMP(),2147483647,UNIX_TIMESTAMP(),?,1,'1') ON DUPLICATE KEY UPDATE login_time=UNIX_TIMESTAMP(),expire_time=2147483647,last_play_time=UNIX_TIMESTAMP(),login_ip=VALUES(login_ip),cleanpad_point=1,tutorial_skipable='1'"
	return run(stmtSQL, "upsert member_login", uid, loginIP)
}

func runOnlineRepairSQL(ctx context.Context, db *sql.DB, query string, step string, args ...interface{}) bool {
	if db == nil {
		fmt.Printf("MsgOnLine preflight sql failed: %s (no db)\n", step)
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		fmt.Printf("MsgOnLine preflight sql failed: %s: %v\n", step, err)
		return false
	}
	return true
}
