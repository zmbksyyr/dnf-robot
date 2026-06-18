package service

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func (m *RobotManager) selectRobots(req RobotCommandRequest) ([]RobotInfo, error) {
	var rows *sql.Rows
	var err error
	if len(req.UIDs) > 0 {
		holders := strings.TrimRight(strings.Repeat("?,", len(req.UIDs)), ",")
		args := make([]interface{}, len(req.UIDs))
		for i, uid := range req.UIDs {
			args[i] = uid
		}
		rows, err = m.db.Query("SELECT d.UID,d.CID,d.port,d.curvill,d.curarea,d.curx,d.cury,IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0) FROM d_starsky.Dummylist d LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=d.CID WHERE d.UID IN ("+holders+") ORDER BY CAST(d.UID AS UNSIGNED)", args...)
	} else {
		if req.Count <= 0 {
			req.Count = 10
		}
		rows, err = m.db.Query("SELECT d.UID,d.CID,d.port,d.curvill,d.curarea,d.curx,d.cury,IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0) FROM d_starsky.Dummylist d LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=d.CID ORDER BY CAST(d.UID AS UNSIGNED) LIMIT ?", req.Count)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RobotInfo
	for rows.Next() {
		var r RobotInfo
		if err := rows.Scan(&r.UID, &r.CID, &r.Port, &r.Village, &r.Area, &r.X, &r.Y, &r.Level, &r.Job, &r.Grow); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (m *RobotManager) insertIgnore(table string, values map[string]interface{}) error {
	cols, err := m.tableColumns(table)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values))
	for k, v := range values {
		if cols[k] {
			names = append(names, k)
			args = append(args, v)
		}
	}
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, len(names))
	holders := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
		holders[i] = "?"
	}
	_, err = m.db.Exec("INSERT IGNORE INTO "+quoteTable(table)+" ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(holders, ",")+")", args...)
	return err
}

func (m *RobotManager) tableColumns(table string) (map[string]bool, error) {
	m.cacheMu.Lock()
	if c, ok := m.colCache[table]; ok {
		m.cacheMu.Unlock()
		return c, nil
	}
	m.cacheMu.Unlock()
	rows, err := m.db.Query("SHOW COLUMNS FROM " + quoteTable(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var field, typ, null, key string
		var def sql.NullString
		var extra string
		if err := rows.Scan(&field, &typ, &null, &key, &def, &extra); err != nil {
			return nil, err
		}
		cols[field] = true
	}
	m.cacheMu.Lock()
	m.colCache[table] = cols
	if len(m.colCache) > 100 {
		for k := range m.colCache {
			if k != table {
				delete(m.colCache, k)
				break
			}
		}
	}
	m.cacheMu.Unlock()
	return cols, rows.Err()
}

func quoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i := range parts {
		parts[i] = "`" + parts[i] + "`"
	}
	return strings.Join(parts, ".")
}

func (m *RobotManager) nextInt(query string, fallback int) (int, error) {
	var v sql.NullInt64
	err := m.db.QueryRow(query).Scan(&v)
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	if !v.Valid {
		return fallback, nil
	}
	return int(v.Int64), nil
}

func (m *RobotManager) availableRobotUIDs(count, start, maxExclusive int) ([]int, error) {
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
		if err := m.addUsedUIDs(used, src.table, src.column, start); err != nil {
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

func (m *RobotManager) accountAutoIncrement() (int, error) {
	var v sql.NullInt64
	err := m.db.QueryRow("SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='d_taiwan' AND TABLE_NAME='accounts' LIMIT 1").Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (m *RobotManager) addUsedUIDs(used map[int]bool, table, column string, start int) error {
	cols, err := m.tableColumns(table)
	if err != nil || !cols[column] {
		return nil
	}
	rows, err := m.db.Query("SELECT `"+column+"` FROM "+quoteTable(table)+" WHERE CAST(`"+column+"` AS UNSIGNED) >= ?", start)
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
