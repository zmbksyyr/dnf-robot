package dnf

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"robot/internal/foundation/lockhub"
)

const loginRepairSchemaTimeout = 5 * time.Second

type loginRepairCapabilities struct {
	dTaiwanMemberJoinInfo          bool
	taiwanLoginMemberJoinInfo      bool
	dTaiwanMemberSecurityGrade     bool
	dTaiwanSecuMemberSecurityGrade bool
	memberPunishInfo               bool
	robotRegistry                  bool
	expLevelRefPopulated           bool
}

func (c loginRepairCapabilities) memberJoinInfoTables() []string {
	tables := make([]string, 0, 2)
	if c.dTaiwanMemberJoinInfo {
		tables = append(tables, "d_taiwan.member_join_info")
	}
	if c.taiwanLoginMemberJoinInfo {
		tables = append(tables, "taiwan_login.member_join_info")
	}
	return tables
}

func (c loginRepairCapabilities) memberSecurityGradeTables() []string {
	tables := make([]string, 0, 2)
	if c.dTaiwanMemberSecurityGrade {
		tables = append(tables, "d_taiwan.member_security_grade")
	}
	if c.dTaiwanSecuMemberSecurityGrade {
		tables = append(tables, "d_taiwan_secu.member_security_grade")
	}
	return tables
}

type loginRepairCapabilityCache struct {
	access       lockhub.Locker
	db           *sql.DB
	loaded       bool
	capabilities loginRepairCapabilities
}

func (c *loginRepairCapabilityCache) get(db *sql.DB, load func(*sql.DB) (loginRepairCapabilities, error)) (loginRepairCapabilities, error) {
	if db == nil {
		return loginRepairCapabilities{}, fmt.Errorf("database is not configured")
	}
	c.access.Lock()
	defer c.access.Unlock()
	if c.loaded && c.db == db {
		return c.capabilities, nil
	}
	capabilities, err := load(db)
	if err != nil {
		return loginRepairCapabilities{}, err
	}
	c.db = db
	c.loaded = true
	c.capabilities = capabilities
	return capabilities, nil
}

var loginRepairSchemaCache loginRepairCapabilityCache

func loginRepairCapabilitiesFor(db *sql.DB) (loginRepairCapabilities, error) {
	return loginRepairSchemaCache.get(db, inspectLoginRepairCapabilities)
}

func inspectLoginRepairCapabilities(db *sql.DB) (loginRepairCapabilities, error) {
	ctx, cancel := context.WithTimeout(context.Background(), loginRepairSchemaTimeout)
	defer cancel()

	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS d_taiwan.member_info_bot_backup LIKE d_taiwan.member_info"); err != nil {
		return loginRepairCapabilities{}, fmt.Errorf("create member_info_bot_backup: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT TABLE_SCHEMA,TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA IN ('d_taiwan','taiwan_login','d_taiwan_secu','d_starsky','taiwan_cain')
		AND TABLE_NAME IN ('member_join_info','member_security_grade','member_punish_info','robot_registry','exp_level_ref')`)
	if err != nil {
		return loginRepairCapabilities{}, fmt.Errorf("query optional login tables: %w", err)
	}

	available := make(map[string]bool, 7)
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return loginRepairCapabilities{}, fmt.Errorf("scan optional login table: %w", err)
		}
		available[schema+"."+table] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return loginRepairCapabilities{}, fmt.Errorf("read optional login tables: %w", err)
	}
	if err := rows.Close(); err != nil {
		return loginRepairCapabilities{}, fmt.Errorf("close optional login tables: %w", err)
	}

	capabilities := loginRepairCapabilities{
		dTaiwanMemberJoinInfo:          available["d_taiwan.member_join_info"],
		taiwanLoginMemberJoinInfo:      available["taiwan_login.member_join_info"],
		dTaiwanMemberSecurityGrade:     available["d_taiwan.member_security_grade"],
		dTaiwanSecuMemberSecurityGrade: available["d_taiwan_secu.member_security_grade"],
		memberPunishInfo:               available["d_taiwan.member_punish_info"],
		robotRegistry:                  available["d_starsky.robot_registry"],
	}
	if available["taiwan_cain.exp_level_ref"] {
		var populated int
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM taiwan_cain.exp_level_ref LIMIT 1)").Scan(&populated); err != nil {
			return loginRepairCapabilities{}, fmt.Errorf("inspect exp_level_ref: %w", err)
		}
		capabilities.expLevelRefPopulated = populated != 0
	}
	return capabilities, nil
}
