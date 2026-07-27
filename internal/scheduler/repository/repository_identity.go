package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	lifecyclecap "robot/internal/capability/robotlifecycle"
	"sort"
	"strconv"
	"strings"
)

var robotUIDSources = []struct {
	table  string
	column string
}{
	{"d_taiwan.accounts", "UID"},
	{"d_taiwan.limit_create_character", "m_id"},
	{"d_taiwan.member_info", "m_id"},
	{"d_taiwan.member_info_bot_backup", "m_id"},
	{"taiwan_login.member_login", "m_id"},
	{"taiwan_login.member_game_option", "m_id"},
	{"taiwan_login.member_premium", "m_id"},
	{"taiwan_login.dnf_event_entry", "m_id"},
	{"taiwan_login_play.member_key_option", "m_id"},
	{"taiwan_cain.charac_info", "m_id"},
	{"taiwan_cain.charac_view", "m_id"},
	{"taiwan_cain.charac_link_message", "m_id"},
	{"taiwan_cain.member_dungeon", "m_id"},
	{"d_starsky.Dummylist", "UID"},
	{"d_starsky.v4_ai_user", "uid"},
	{"d_starsky.robot_registry", "uid"},
	{"d_starsky.Robot_stall", "UID"},
	{"d_starsky.Robot_stall_config", "UID"},
}

func (r *SQLRepository) AvailableRobotUIDs(count, start, end int) ([]int, error) {
	if count <= 0 {
		return nil, nil
	}
	if start <= 0 {
		start = 17000000
	}
	if end < start {
		end = start + 999999
	}
	used := make(map[int]bool)
	for _, src := range robotUIDSources {
		if err := r.addUsedUIDs(used, src.table, src.column, start, end); err != nil {
			return nil, err
		}
	}
	out := make([]int, 0, count)
	for uid := start; len(out) < count && uid <= end; uid++ {
		if !used[uid] {
			out = append(out, uid)
		}
	}
	if len(out) < count {
		return nil, fmt.Errorf("not enough free robot uid in robot segment %d-%d; requested=%d free=%d", start, end, count, len(out))
	}
	return out, nil
}

func (r *SQLRepository) PrepareRobotUIDRange(start, end, guard int) error {
	if start <= 0 || end < start {
		return fmt.Errorf("invalid robot uid segment %d-%d", start, end)
	}
	used := make(map[int]bool)
	for _, src := range robotUIDSources {
		if err := r.addUsedUIDs(used, src.table, src.column, start, end); err != nil {
			return err
		}
	}
	owned := make(map[int]bool)
	rows, err := r.Query("SELECT uid FROM d_starsky.robot_registry WHERE uid BETWEEN ? AND ?", start, end)
	if err != nil {
		return err
	}
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		owned[uid] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	conflicts := make([]int, 0)
	for uid := range used {
		if !owned[uid] {
			conflicts = append(conflicts, uid)
		}
	}
	if len(conflicts) > 0 {
		sort.Ints(conflicts)
		if len(conflicts) > 10 {
			conflicts = conflicts[:10]
		}
		return fmt.Errorf("robot uid segment %d-%d contains non-robot uid(s) %v; choose another segment before creating robots", start, end, conflicts)
	}
	if guard == 0 {
		return nil
	}
	if guard <= end {
		return fmt.Errorf("robot uid guard %d must be greater than segment end %d", guard, end)
	}
	guardAccount := fmt.Sprintf("robotguard%d", guard)
	var existing string
	err = r.QueryRow("SELECT accountname FROM d_taiwan.accounts WHERE UID=? LIMIT 1", guard).Scan(&existing)
	if err == nil {
		if existing != guardAccount {
			return fmt.Errorf("robot uid guard %d is occupied by account %q", guard, existing)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	randomPassword := make([]byte, 16)
	if _, err := rand.Read(randomPassword); err != nil {
		return fmt.Errorf("generate robot uid guard password: %w", err)
	}
	if err := r.InsertIgnore("d_taiwan.accounts", map[string]interface{}{
		"UID": guard, "accountname": guardAccount, "password": hex.EncodeToString(randomPassword),
		"qq": "", "VIP": "", "ip": "", "login_IP": "", "login_Mac": "",
		"seal_IP": 1, "seal_MAC": 1, "seal_accountname": 1,
	}); err != nil {
		return err
	}
	existing = ""
	if err := r.QueryRow("SELECT accountname FROM d_taiwan.accounts WHERE UID=? LIMIT 1", guard).Scan(&existing); err != nil {
		return err
	}
	if existing != guardAccount {
		return fmt.Errorf("robot uid guard %d was concurrently occupied by account %q", guard, existing)
	}
	return nil
}

func (r *SQLRepository) AccountAutoIncrement() (int, error) {
	var v sql.NullInt64
	err := r.QueryRow("SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='d_taiwan' AND TABLE_NAME='accounts' LIMIT 1").Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (r *SQLRepository) AllocateRobotIDs(count, uidStart, uidEnd int) (lifecyclecap.RobotIDAllocation, error) {
	uids, err := r.AvailableRobotUIDs(count, uidStart, uidEnd)
	if err != nil {
		return lifecyclecap.RobotIDAllocation{}, err
	}
	nextCID, err := r.NextInt("SELECT charac_no FROM taiwan_cain.charac_info ORDER BY charac_no DESC LIMIT 1", 0)
	if err != nil {
		return lifecyclecap.RobotIDAllocation{}, err
	}
	return lifecyclecap.RobotIDAllocation{UIDs: uids, FirstCID: nextCID + 1}, nil
}

func (r *SQLRepository) CharacterNameExists(dbName string) (bool, error) {
	var x int
	err := r.QueryRow("SELECT 1 FROM taiwan_cain.charac_info WHERE charac_name=? LIMIT 1", dbName).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return x == 1, nil
}

func (r *SQLRepository) addUsedUIDs(used map[int]bool, table, column string, start, end int) error {
	cols, err := r.TableColumns(table)
	if err != nil || !cols[column] {
		return nil
	}
	rows, err := r.Query("SELECT `"+column+"` FROM "+quoteTable(table)+" WHERE CAST(`"+column+"` AS UNSIGNED) BETWEEN ? AND ?", start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if !raw.Valid {
			continue
		}
		uid, err := strconv.Atoi(strings.TrimSpace(raw.String))
		if err == nil && uid >= start && uid <= end {
			used[uid] = true
		}
	}
	return rows.Err()
}
