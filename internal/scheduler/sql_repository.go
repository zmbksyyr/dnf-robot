package scheduler

import (
	"database/sql"

	schedulerrepo "robot/internal/scheduler/repository"
)

func NewSQLRepository(db *sql.DB) Database {
	return schedulerrepo.NewSQLRepository(db)
}
