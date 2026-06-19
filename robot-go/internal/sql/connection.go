package sql

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

func NewConnection(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Select(db *sql.DB, query string, args ...interface{}) ([][]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringRows(rows)
}

func Exec(db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	return db.Exec(query, args...)
}

func StmtSelect(db *sql.DB, query string, args ...interface{}) ([][]string, error) {
	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	return SelectViaStmt(stmt, args...)
}

func SelectViaStmt(stmt *sql.Stmt, args ...interface{}) ([][]string, error) {
	rows, err := stmt.Query(args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringRows(rows)
}

func scanStringRows(rows *sql.Rows) ([][]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result [][]string
	rawValues := make([]interface{}, len(cols))
	strValues := make([]sql.NullString, len(cols))
	for i := range rawValues {
		rawValues[i] = &strValues[i]
	}

	for rows.Next() {
		if err := rows.Scan(rawValues...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range strValues {
			if v.Valid {
				row[i] = v.String
			} else {
				row[i] = ""
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
