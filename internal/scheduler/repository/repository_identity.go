package repository

import (
	"database/sql"
	"fmt"
	lifecyclecap "robot/internal/capability/robotlifecycle"
	"strconv"
	"strings"
)

func (r *SQLRepository) AvailableRobotUIDs(count, start, maxExclusive int) ([]int, error) {
	if count <= 0 {
		return nil, nil
	}
	if start <= 0 {
		start = 17000000
	}
	used := make(map[int]bool)
	uidSources := []struct {
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
	for _, src := range uidSources {
		if err := r.addUsedUIDs(used, src.table, src.column, start); err != nil {
			return nil, err
		}
	}
	out := make([]int, 0, count)
	for uid := start; len(out) < count; uid++ {
		if maxExclusive > 0 && uid >= maxExclusive {
			return nil, fmt.Errorf("not enough free robot uid below d_taiwan.accounts AUTO_INCREMENT=%d; robot_uid_start=%d requested=%d free=%d", maxExclusive, start, count, len(out))
		}
		if !used[uid] {
			out = append(out, uid)
		}
	}
	return out, nil
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

func (r *SQLRepository) AllocateRobotIDs(count, uidStart int) (lifecyclecap.RobotIDAllocation, error) {
	accountAutoInc, err := r.AccountAutoIncrement()
	if err != nil {
		return lifecyclecap.RobotIDAllocation{}, err
	}
	uids, err := r.AvailableRobotUIDs(count, uidStart, accountAutoInc)
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

func (r *SQLRepository) addUsedUIDs(used map[int]bool, table, column string, start int) error {
	cols, err := r.TableColumns(table)
	if err != nil || !cols[column] {
		return nil
	}
	rows, err := r.Query("SELECT `"+column+"` FROM "+quoteTable(table)+" WHERE CAST(`"+column+"` AS UNSIGNED) >= ?", start)
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
		if err == nil && uid >= start {
			used[uid] = true
		}
	}
	return rows.Err()
}
