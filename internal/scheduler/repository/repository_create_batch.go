package repository

import (
	"encoding/json"
	"fmt"
	"strings"
)

type createBatchPayload struct {
	UIDs []int `json:"uids"`
	CIDs []int `json:"cids"`
}

func (r *SQLRepository) BeginCreateBatch(batchID string, uids, cids []int) error {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || len(uids) == 0 || len(uids) != len(cids) {
		return fmt.Errorf("invalid robot creation batch")
	}
	payload, err := json.Marshal(createBatchPayload{UIDs: uids, CIDs: cids})
	if err != nil {
		return err
	}
	_, err = r.Exec("INSERT INTO d_starsky.robot_create_batch (batch_id,payload,state,created_at,updated_at) VALUES (?,?,'running',NOW(),NOW())", batchID, string(payload))
	return err
}

func (r *SQLRepository) CompleteCreateBatch(batchID string) error {
	result, err := r.Exec("UPDATE d_starsky.robot_create_batch SET state='complete',updated_at=NOW() WHERE batch_id=? AND state='running'", batchID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("robot creation batch %q is not running", batchID)
	}
	return nil
}

func (r *SQLRepository) RollbackCreateBatch(batchID string) error {
	var raw string
	if err := r.QueryRow("SELECT payload FROM d_starsky.robot_create_batch WHERE batch_id=? AND state='running' LIMIT 1", batchID).Scan(&raw); err != nil {
		return err
	}
	var payload createBatchPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Errorf("decode robot creation batch %q: %w", batchID, err)
	}
	if err := r.BatchDeleteRobotData(payload.UIDs, payload.CIDs); err != nil {
		return fmt.Errorf("rollback robot creation batch %q: %w", batchID, err)
	}
	_, err := r.Exec("UPDATE d_starsky.robot_create_batch SET state='rolled_back',updated_at=NOW() WHERE batch_id=? AND state='running'", batchID)
	return err
}

func (r *SQLRepository) RecoverIncompleteCreateBatches() error {
	rows, err := r.Query("SELECT batch_id FROM d_starsky.robot_create_batch WHERE state='running' ORDER BY created_at,batch_id")
	if err != nil {
		return err
	}
	var batches []string
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return err
		}
		batches = append(batches, batchID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, batchID := range batches {
		if err := r.RollbackCreateBatch(batchID); err != nil {
			return err
		}
	}
	return nil
}
