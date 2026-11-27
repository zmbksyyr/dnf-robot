package repository

import (
	"fmt"

	equipcap "robot/internal/capability/equipment"
	storecap "robot/internal/capability/store"
)

// PrepareCharacterSupplies runs after the previous session has saved and
// unloaded, before login loads inventory and fatigue into the original game.
func (r *SQLRepository) PrepareCharacterSupplies(uid, cid int) error {
	tx, err := r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(`SELECT UNCOMPRESS(i.inventory)
		FROM taiwan_cain_2nd.inventory i
		JOIN taiwan_cain.charac_info c ON c.charac_no=i.charac_no
		WHERE c.m_id=? AND c.charac_no=? AND c.delete_flag=0 FOR UPDATE`, uid, cid).Scan(&raw); err != nil {
		return fmt.Errorf("load dungeon supplies uid=%d cid=%d: %w", uid, cid, err)
	}
	changed, err := storecap.EnsureDungeonSupplies(raw)
	if err != nil {
		return fmt.Errorf("prepare dungeon supplies uid=%d cid=%d: %w", uid, cid, err)
	}
	if changed {
		if _, err := tx.Exec("UPDATE taiwan_cain_2nd.inventory SET inventory=? WHERE charac_no=?", equipcap.CompressRaw(raw), cid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE taiwan_cain.charac_info SET fatigue=0 WHERE charac_no=? AND m_id=?", cid, uid); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE taiwan_cain.charac_stat SET fatigue=0,used_fatigue=0 WHERE charac_no=?", cid); err != nil {
		return err
	}
	return tx.Commit()
}
