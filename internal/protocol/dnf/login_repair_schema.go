package dnf

import (
	"context"
	"database/sql"
	"fmt"

	"robot/internal/foundation/lockhub"
)

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
	access  lockhub.Locker
	entries map[*sql.DB]*loginRepairCapabilityEntry
}

type loginRepairCapabilityEntry struct {
	done         chan struct{}
	capabilities loginRepairCapabilities
	err          error
}

func (c *loginRepairCapabilityCache) get(ctx context.Context, db *sql.DB, load func(*sql.DB) (loginRepairCapabilities, error)) (loginRepairCapabilities, error) {
	if db == nil {
		return loginRepairCapabilities{}, fmt.Errorf("database is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return loginRepairCapabilities{}, err
	}
	c.access.Lock()
	if entry := c.entries[db]; entry != nil {
		c.access.Unlock()
		select {
		case <-entry.done:
			return entry.capabilities, entry.err
		case <-ctx.Done():
			return loginRepairCapabilities{}, ctx.Err()
		}
	}
	if c.entries == nil {
		c.entries = make(map[*sql.DB]*loginRepairCapabilityEntry)
	}
	entry := &loginRepairCapabilityEntry{done: make(chan struct{})}
	c.entries[db] = entry
	c.access.Unlock()

	capabilities, err := load(db)
	c.access.Lock()
	entry.capabilities = capabilities
	entry.err = err
	if err != nil {
		delete(c.entries, db)
	}
	close(entry.done)
	c.access.Unlock()
	return entry.capabilities, entry.err
}

var loginRepairSchemaCache loginRepairCapabilityCache

func loginRepairCapabilitiesFor(ctx context.Context, db *sql.DB) (loginRepairCapabilities, error) {
	return loginRepairSchemaCache.get(ctx, db, func(db *sql.DB) (loginRepairCapabilities, error) {
		return inspectLoginRepairCapabilities(ctx, db)
	})
}

func inspectLoginRepairCapabilities(ctx context.Context, db *sql.DB) (loginRepairCapabilities, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
