package repository

import (
	"database/sql"
	"fmt"
	robotcap "robot/internal/capability/robot"
	"strings"
)

const maxDangerousUIDRange = 10000

func (r *SQLRepository) DangerousDeletePlan(req robotcap.DangerousDeleteRequest) (robotcap.DangerousDeletePlan, error) {
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	switch req.Mode {
	case robotcap.DangerousDeleteModeCID:
		return r.dangerousCIDPlan(req.CID)
	case robotcap.DangerousDeleteModeUID:
		return r.dangerousUIDPlan(req.UID)
	case robotcap.DangerousDeleteModeRange:
		return r.dangerousRangePlan(req.MinUID, req.MaxUID)
	default:
		return robotcap.DangerousDeletePlan{}, fmt.Errorf("invalid dangerous delete mode %q", req.Mode)
	}
}

func (r *SQLRepository) dangerousCIDPlan(cid int) (robotcap.DangerousDeletePlan, error) {
	if cid <= 0 {
		return robotcap.DangerousDeletePlan{}, fmt.Errorf("cid must be positive")
	}
	plan := robotcap.DangerousDeletePlan{Mode: robotcap.DangerousDeleteModeCID, CID: cid, CIDs: []int{cid}, CharacterCount: 1}
	if err := r.QueryRow("SELECT m_id,charac_name FROM taiwan_cain.charac_info WHERE charac_no=? LIMIT 1", cid).Scan(&plan.UID, &plan.CharacterName); err != nil {
		if err == sql.ErrNoRows {
			return robotcap.DangerousDeletePlan{}, fmt.Errorf("character cid %d not found", cid)
		}
		return robotcap.DangerousDeletePlan{}, err
	}
	if err := r.QueryRow("SELECT COUNT(*) FROM d_taiwan.accounts WHERE UID=?", plan.UID).Scan(&plan.AccountCount); err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	registryUIDs, err := r.queryIntColumn("SELECT uid FROM d_starsky.robot_registry WHERE uid=? AND cid=?", plan.UID, cid)
	if err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.RegistryUIDs = registryUIDs
	plan.RegistryCount = len(registryUIDs)
	return plan, nil
}

func (r *SQLRepository) dangerousUIDPlan(uid int) (robotcap.DangerousDeletePlan, error) {
	if uid <= 0 {
		return robotcap.DangerousDeletePlan{}, fmt.Errorf("uid must be positive")
	}
	plan := robotcap.DangerousDeletePlan{Mode: robotcap.DangerousDeleteModeUID, UID: uid, UIDs: []int{uid}}
	var err error
	plan.CIDs, err = r.queryIntColumn("SELECT charac_no FROM taiwan_cain.charac_info WHERE m_id=?", uid)
	if err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.CharacterCount = len(plan.CIDs)
	if err := r.QueryRow("SELECT COUNT(*) FROM d_taiwan.accounts WHERE UID=?", uid).Scan(&plan.AccountCount); err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.RegistryUIDs, err = r.queryIntColumn("SELECT uid FROM d_starsky.robot_registry WHERE uid=?", uid)
	if err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.RegistryCount = len(plan.RegistryUIDs)
	return plan, nil
}

func (r *SQLRepository) dangerousRangePlan(minUID, maxUID int) (robotcap.DangerousDeletePlan, error) {
	if minUID <= 0 || maxUID <= 0 || minUID > maxUID {
		return robotcap.DangerousDeletePlan{}, fmt.Errorf("invalid uid range %d-%d", minUID, maxUID)
	}
	if maxUID-minUID+1 > maxDangerousUIDRange {
		return robotcap.DangerousDeletePlan{}, fmt.Errorf("uid range exceeds maximum %d", maxDangerousUIDRange)
	}
	plan := robotcap.DangerousDeletePlan{Mode: robotcap.DangerousDeleteModeRange, MinUID: minUID, MaxUID: maxUID}
	plan.UIDs = make([]int, 0, maxUID-minUID+1)
	for uid := minUID; uid <= maxUID; uid++ {
		plan.UIDs = append(plan.UIDs, uid)
	}
	var err error
	plan.CIDs, err = r.queryIntColumn("SELECT charac_no FROM taiwan_cain.charac_info WHERE m_id BETWEEN ? AND ?", minUID, maxUID)
	if err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.CharacterCount = len(plan.CIDs)
	if err := r.QueryRow("SELECT COUNT(*) FROM d_taiwan.accounts WHERE UID BETWEEN ? AND ?", minUID, maxUID).Scan(&plan.AccountCount); err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.RegistryUIDs, err = r.queryIntColumn("SELECT uid FROM d_starsky.robot_registry WHERE uid BETWEEN ? AND ?", minUID, maxUID)
	if err != nil {
		return robotcap.DangerousDeletePlan{}, err
	}
	plan.RegistryCount = len(plan.RegistryUIDs)
	return plan, nil
}

func (r *SQLRepository) queryIntColumn(query string, args ...interface{}) ([]int, error) {
	rows, err := r.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
