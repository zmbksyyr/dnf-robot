package repository

import (
	"database/sql"
	robotcap "robot/internal/capability/robot"
	foundsql "robot/internal/foundation/sql"
)

func (r *SQLRepository) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	var rows *sql.Rows
	var err error
	if len(req.UIDs) > 0 {
		holders := foundsql.Placeholders(len(req.UIDs))
		args := make([]interface{}, len(req.UIDs))
		for i, uid := range req.UIDs {
			args[i] = uid
		}
		rows, err = r.Query("SELECT d.UID,d.CID,IFNULL(c.charac_name,''),d.port,d.curvill,d.curarea,d.curx,d.cury,IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0) FROM d_starsky.Dummylist d LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=d.CID WHERE d.UID IN ("+holders+") ORDER BY CAST(d.UID AS UNSIGNED)", args...)
	} else {
		if req.Count <= 0 {
			req.Count = 10
		}
		rows, err = r.Query("SELECT d.UID,d.CID,IFNULL(c.charac_name,''),d.port,d.curvill,d.curarea,d.curx,d.cury,IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0) FROM d_starsky.Dummylist d LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=d.CID ORDER BY CAST(d.UID AS UNSIGNED) LIMIT ?", req.Count)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []robotcap.Info
	for rows.Next() {
		var r robotcap.Info
		if err := rows.Scan(&r.UID, &r.CID, &r.Name, &r.Port, &r.Village, &r.Area, &r.X, &r.Y, &r.Level, &r.Job, &r.Grow); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (r *SQLRepository) UpdateRobotPosition(info robotcap.Info, x, y int) error {
	_, err := r.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=? WHERE UID=?", info.Village, info.Area, x, y, info.UID)
	return err
}

func (r *SQLRepository) FollowAccountUIDs(account string) ([]int, error) {
	rows, err := r.Query(`SELECT uid FROM d_starsky.robot_registry WHERE account=? ORDER BY uid DESC LIMIT 20`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var uids []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}
	return uids, rows.Err()
}

func (r *SQLRepository) FollowAccountVillageLastPlayed(account string) (int, bool, error) {
	var village sql.NullInt64
	err := r.QueryRow(`
SELECT COALESCE(NULLIF(s.village,0), c.village)
FROM d_taiwan.accounts a
JOIN taiwan_cain.charac_info c ON c.m_id=a.UID AND c.delete_flag=0
LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=c.charac_no
WHERE a.accountname=?
ORDER BY c.last_play_time DESC, c.charac_no DESC
LIMIT 1`, account).Scan(&village)
	if err != nil || !village.Valid || village.Int64 <= 0 {
		return 0, false, err
	}
	return int(village.Int64), true, nil
}

func (r *SQLRepository) RobotCharacterName(uid int) (string, error) {
	var name string
	err := r.QueryRow("SELECT IFNULL(charac_name,'') FROM taiwan_cain.charac_info WHERE m_id=? AND delete_flag=0 ORDER BY last_play_time DESC, charac_no DESC LIMIT 1", uid).Scan(&name)
	return name, err
}

func (r *SQLRepository) AliveRobotUIDs(uids []int) (map[int]bool, error) {
	alive := make(map[int]bool, len(uids))
	if len(uids) == 0 {
		return alive, nil
	}
	holders := foundsql.Placeholders(len(uids))
	args := make([]interface{}, len(uids))
	for i, uid := range uids {
		args[i] = uid
	}
	rows, err := r.Query("SELECT m_id FROM taiwan_cain.charac_info WHERE delete_flag=0 AND m_id IN ("+holders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		alive[uid] = true
	}
	return alive, rows.Err()
}

func (r *SQLRepository) RobotStatusRows(req robotcap.CommandRequest) ([]robotcap.StatusItem, error) {
	args := make([]interface{}, 0)
	where := ""
	limit := ""
	if len(req.UIDs) > 0 {
		holders := foundsql.Placeholders(len(req.UIDs))
		where = "WHERE r.uid IN (" + holders + ")"
		for _, uid := range req.UIDs {
			args = append(args, uid)
		}
	} else {
		if req.Count <= 0 {
			req.Count = 500
		}
		limit = " LIMIT ?"
		args = append(args, req.Count)
	}
	query := `
SELECT r.uid,r.cid,IFNULL(r.charac_name,''),IFNULL(r.account,''),IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0),
       IF(c.charac_no IS NULL,0,1),
       IFNULL(d.curvill,0),IFNULL(d.curarea,0),IFNULL(d.curx,0),IFNULL(d.cury,0)
FROM d_starsky.robot_registry r
LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=r.cid AND c.delete_flag=0
LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid
` + where + `
ORDER BY r.uid` + limit
	rows, err := r.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []robotcap.StatusItem
	for rows.Next() {
		var item robotcap.StatusItem
		var coreAlive int
		if err := rows.Scan(&item.UID, &item.CID, &item.Name, &item.Account, &item.Level, &item.Job, &item.Grow, &coreAlive, &item.Village, &item.Area, &item.X, &item.Y); err != nil {
			return nil, err
		}
		if coreAlive == 0 {
			item.MissingCore = true
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
