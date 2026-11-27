package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"robot/internal/foundation/config"
)

// Run against a disposable MySQL instance with strict mode and zero-date
// restrictions enabled. All writes use connection-local temporary tables.
func TestDatabaseLegacyDatesWithStrictMySQL(t *testing.T) {
	dsn := os.Getenv("DNF_ROBOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("DNF_ROBOT_TEST_MYSQL_DSN is not set")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test MySQL DSN")
	}
	host, portText, err := net.SplitHostPort(parsed.Addr)
	if err != nil || parsed.Net != "tcp" {
		t.Fatal("test MySQL DSN must use TCP")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var before string
	if err := raw.QueryRow("SELECT @@GLOBAL.sql_mode").Scan(&before); err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for _, mode := range strings.Split(before, ",") {
		if mode != "NO_ZERO_DATE" && mode != "NO_ZERO_IN_DATE" {
			want[mode] = true
		}
	}
	if !want["STRICT_TRANS_TABLES"] && !want["STRICT_ALL_TABLES"] {
		t.Fatal("test server must enable strict mode")
	}
	if !strings.Contains(before, "NO_ZERO_DATE") {
		t.Fatal("test server must enable NO_ZERO_DATE")
	}
	db, err := openDatabase(&config.SysConfig{
		DBHost: host, DBPort: port, DBName: parsed.DBName, DBUser: parsed.User, DBPassword: parsed.Passwd,
		DBMaxSize: 2, DBInitSize: 2, DBDialTimeoutSec: 5, DBReadTimeoutSec: 10, DBWriteTimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	connections := make([]*sql.Conn, 0, 2)
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	// Hold two simultaneous connections, then force a fresh connection. The
	// compatibility mode must be initialized for every physical connection.
	for i := 0; i < 3; i++ {
		if i == 2 {
			for _, conn := range connections {
				_ = conn.Close()
			}
			connections = connections[:0]
			db.SetMaxIdleConns(0)
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		var modes string
		if err := conn.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&modes); err != nil {
			t.Fatal(err)
		}
		got := make(map[string]bool)
		for _, mode := range strings.Split(modes, ",") {
			got[mode] = true
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("session modes = %q; global baseline = %q", modes, before)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TEMPORARY TABLE robot_zero_date_test (n INT NOT NULL, d DATETIME NOT NULL DEFAULT '0000-00-00 00:00:00')"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO robot_zero_date_test (n) VALUES (7)"); err != nil {
			t.Fatal(err)
		}
		var n int
		var date string
		if err := conn.QueryRowContext(ctx, "SELECT n,CAST(d AS CHAR) FROM robot_zero_date_test").Scan(&n, &date); err != nil || n != 7 || date != "0000-00-00 00:00:00" {
			t.Fatalf("legacy unset date changed: n=%d date=%q err=%v", n, date, err)
		}
		_, err = conn.ExecContext(ctx, "INSERT INTO robot_zero_date_test (n) VALUES ('invalid')")
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1366 {
			t.Fatalf("strict integer validation lost: %v", err)
		}
	}
	var after string
	if err := raw.QueryRow("SELECT @@GLOBAL.sql_mode").Scan(&after); err != nil || after != before {
		t.Fatalf("global SQL mode changed: before=%q after=%q err=%v", before, after, err)
	}
}
