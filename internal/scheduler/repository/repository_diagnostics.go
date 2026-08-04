package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"

	"robot/internal/foundation/config"
)

// ReadRobotRegistryUIDs returns the complete UID list used by the Web robot table.
func ReadRobotRegistryUIDs(cfg *config.SysConfig) ([]uint32, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config is nil")
	}
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = cfg.DBUser
	mysqlCfg.Passwd = cfg.DBPassword
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(cfg.DBHost, strconv.Itoa(cfg.DBPort))
	mysqlCfg.DBName = cfg.DBName
	mysqlCfg.Params = map[string]string{"charset": "utf8"}
	mysqlCfg.Timeout = 5 * time.Second
	mysqlCfg.ReadTimeout = 5 * time.Second
	mysqlCfg.WriteTimeout = 5 * time.Second

	db, err := sql.Open("mysql", mysqlCfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT uid FROM d_starsky.robot_registry ORDER BY uid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uids := make([]uint32, 0)
	for rows.Next() {
		var uid uint64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		if uid == 0 || uid > math.MaxUint32 {
			return nil, fmt.Errorf("robot_registry contains invalid uid %d", uid)
		}
		uids = append(uids, uint32(uid))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uids, nil
}
