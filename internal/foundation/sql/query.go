package sql

import (
	"database/sql"
	"strings"
)

func Select(db *sql.DB, query string, args ...interface{}) ([][]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringRows(rows)
}

func scanStringRows(rows *sql.Rows) ([][]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result [][]string
	rawValues := make([]interface{}, len(columns))
	stringValues := make([]sql.NullString, len(columns))
	for i := range rawValues {
		rawValues[i] = &stringValues[i]
	}

	for rows.Next() {
		if err := rows.Scan(rawValues...); err != nil {
			return nil, err
		}
		row := make([]string, len(columns))
		for i, value := range stringValues {
			if value.Valid {
				row[i] = value.String
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
