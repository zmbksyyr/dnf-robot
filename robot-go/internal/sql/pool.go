package sql

import (
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type ConnectionPool struct {
	db *sql.DB
}

func NewConnectionPool(dsn string, maxConns int) (*ConnectionPool, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)
	if maxConns/2 < 2 {
		db.SetMaxIdleConns(2)
	}
	db.SetConnMaxIdleTime(60 * time.Second)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return &ConnectionPool{db: db}, nil
}

func (cp *ConnectionPool) GetConnection() *sql.DB {
	return cp.db
}

func (cp *ConnectionPool) Close() error {
	return cp.db.Close()
}
