package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	robotcap "robot/internal/capability/robot"
)

const robotPositionBatchSize = 128

func (r *SQLRepository) UpdateRobotPositions(ctx context.Context, updates []robotcap.PositionUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := executeRobotPositionUpdates(ctx, updates, tx.ExecContext); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func executeRobotPositionUpdates(ctx context.Context, updates []robotcap.PositionUpdate, exec func(context.Context, string, ...interface{}) (sql.Result, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for start := 0; start < len(updates); start += robotPositionBatchSize {
		end := start + robotPositionBatchSize
		if end > len(updates) {
			end = len(updates)
		}
		query, args := buildRobotPositionUpdate(updates[start:end])
		if _, err := exec(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

func buildRobotPositionUpdate(updates []robotcap.PositionUpdate) (string, []interface{}) {
	if len(updates) == 0 {
		return "", nil
	}
	var query strings.Builder
	query.Grow(260 + len(updates)*80)
	query.WriteString("UPDATE d_starsky.Dummylist AS d JOIN (")
	args := make([]interface{}, 0, len(updates)*10)
	for index, update := range updates {
		if index == 0 {
			query.WriteString("SELECT ? AS uid,? AS cid,? AS fromvill,? AS fromarea,? AS fromx,? AS fromy,? AS curvill,? AS curarea,? AS curx,? AS cury")
		} else {
			query.WriteString(" UNION ALL SELECT ?,?,?,?,?,?,?,?,?,?")
		}
		args = append(args,
			strconv.Itoa(update.UID),
			strconv.Itoa(update.CID),
			update.FromVillage,
			update.FromArea,
			update.FromX,
			update.FromY,
			update.Village,
			update.Area,
			update.X,
			update.Y,
		)
	}
	query.WriteString(") AS p ON d.UID=p.uid AND d.CID=p.cid SET d.curvill=p.curvill,d.curarea=p.curarea,d.curx=p.curx,d.cury=p.cury WHERE d.function_type='0' AND d.curvill=p.fromvill AND d.curarea=p.fromarea AND d.curx=p.fromx AND d.cury=p.fromy")
	return query.String(), args
}
