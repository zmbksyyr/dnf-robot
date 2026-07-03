package repository

import (
	"database/sql"
	"fmt"
	"strings"
)

func (r *SQLRepository) InsertIgnore(table string, values map[string]interface{}) error {
	cols, err := r.TableColumns(table)
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
	_, err = r.Exec("INSERT IGNORE INTO "+quoteTable(table)+" ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(holders, ",")+")", args...)
	return err
}

func (r *SQLRepository) InsertIgnoreIfTableExists(table string, values map[string]interface{}) error {
	exists, err := r.TableExists(table)
	if err != nil || !exists {
		return err
	}
	return r.InsertIgnore(table, values)
}

func (r *SQLRepository) TableColumns(table string) (map[string]bool, error) {
	var cached map[string]bool
	var ok bool
	r.withCache("table_columns_read", func() {
		cached, ok = r.colCache[table]
	})
	if ok {
		return cached, nil
	}
	rows, err := r.Query("SHOW COLUMNS FROM " + quoteTable(table))
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
	r.withCache("table_columns_write", func() {
		r.colCache[table] = cols
		if len(r.colCache) > 100 {
			for k := range r.colCache {
				if k != table {
					delete(r.colCache, k)
					break
				}
			}
		}
	})
	return cols, rows.Err()
}

func (r *SQLRepository) TableExists(table string) (bool, error) {
	parts := strings.Split(table, ".")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid table name %q", table)
	}
	var name sql.NullString
	err := r.QueryRow("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=? LIMIT 1", parts[0], parts[1]).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name.Valid, nil
}

func (r *SQLRepository) DeleteByIntIfTableExists(table, col string, id int) error {
	exists, err := r.TableExists(table)
	if err != nil || !exists {
		return err
	}
	_, err = r.Exec("DELETE FROM "+quoteTable(table)+" WHERE `"+col+"`=?", id)
	return err
}

func quoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i := range parts {
		parts[i] = "`" + parts[i] + "`"
	}
	return strings.Join(parts, ".")
}

func (r *SQLRepository) NextInt(query string, fallback int) (int, error) {
	var v sql.NullInt64
	err := r.QueryRow(query).Scan(&v)
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

func (r *SQLRepository) FollowAccountVillage(account string) (int, bool, error) {
	var village sql.NullInt64
	err := r.QueryRow(`
SELECT COALESCE(NULLIF(s.village,0), c.village)
FROM d_taiwan.accounts a
JOIN taiwan_cain.charac_info c ON c.m_id=a.UID AND c.delete_flag=0
LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=c.charac_no
WHERE a.accountname=?
ORDER BY c.charac_no DESC
LIMIT 1`, account).Scan(&village)
	if err != nil || !village.Valid {
		return 0, false, err
	}
	return int(village.Int64), true, nil
}

func (r *SQLRepository) EnsureSchema() error {
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
		if _, err := r.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
