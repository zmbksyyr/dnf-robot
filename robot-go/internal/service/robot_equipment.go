package service

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"math/rand"
)

func (m *RobotManager) rebuildCharacView(uid int) error {
	rows, err := m.db.Query("SELECT charac_no,charac_name,lev,job,grow_type FROM taiwan_cain.charac_info WHERE m_id=? AND delete_flag=0 ORDER BY charac_no", uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cinfo struct {
		cid, lev, job, grow int
		name                string
	}
	var chars []cinfo
	for rows.Next() {
		var c cinfo
		if err := rows.Scan(&c.cid, &c.name, &c.lev, &c.job, &c.grow); err != nil {
			return err
		}
		chars = append(chars, c)
	}
	raw := make([]byte, 36*148)
	for slot, c := range chars {
		if slot >= 36 {
			break
		}
		off := slot * 148
		binary.LittleEndian.PutUint32(raw[off:off+4], uint32(c.cid))
		name := windows1252StringBytes(c.name)
		if len(name) > 20 {
			name = name[:20]
		}
		copy(raw[off+4:], name)
		raw[off+28] = byte(c.lev)
		raw[off+29] = byte(c.job)
		raw[off+30] = byte(c.grow)
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(len(raw)))
	_, _ = m.db.Exec("INSERT IGNORE INTO taiwan_cain.charac_view (m_id) VALUES (?)", uid)
	_, err = m.db.Exec("UPDATE taiwan_cain.charac_view SET info=?,slot_effect_count=18,charac_slot_limit=18,hash_key='',charac_count=? WHERE m_id=?", blob, len(chars), uid)
	return err
}

func (m *RobotManager) copyTemplateDefaults(cid int) error {
	var src int
	if err := m.db.QueryRow("SELECT charac_no FROM taiwan_cain.charac_info WHERE charac_no<>? ORDER BY lev DESC,charac_no LIMIT 1", cid).Scan(&src); err != nil {
		return err
	}
	_, _ = m.db.Exec("UPDATE taiwan_cain.charac_info dst JOIN taiwan_cain.charac_info src SET dst.element_resist=src.element_resist,dst.spec_property=src.spec_property,dst.VIP=src.VIP,dst.create_time=src.create_time WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	_, _ = m.db.Exec("UPDATE taiwan_cain.charac_stat dst JOIN taiwan_cain.charac_stat src SET dst.tutorial_flag=src.tutorial_flag,dst.escalade_tutorial_flag=src.escalade_tutorial_flag,dst.open_flag=src.open_flag,dst.luck_point=src.luck_point WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	_, _ = m.db.Exec("UPDATE taiwan_cain_2nd.skill dst JOIN taiwan_cain_2nd.skill src SET dst.skill_slot=src.skill_slot,dst.skill_slot_2nd=src.skill_slot_2nd,dst.skill_command=src.skill_command,dst.script_version=src.script_version WHERE dst.charac_no=? AND src.charac_no=?", cid, src)
	return nil
}

func buildCompressedZeros(length int) []byte {
	if length < 0 {
		length = 0
	}
	raw := make([]byte, length)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(length))
	return blob
}

func (m *RobotManager) equipFromCatalog(cid int, level int, job int, rc robotRuntimeConfig) error {
	items := m.loadEquipmentCatalog()
	if len(items) == 0 {
		return nil
	}
	selected := m.selectEquipment(items, level, job, rc)
	raw := make([]byte, 12*61)
	for slot, item := range selected {
		m.randMu.Lock()
		writeEquipSlot(raw[(slot-1)*61:slot*61], item, m.rand, rc)
		m.randMu.Unlock()
	}
	_, err := m.db.Exec("UPDATE taiwan_cain_2nd.inventory SET equipslot=? WHERE charac_no=?", compressRaw(raw), cid)
	return err
}

func (m *RobotManager) avatarFromCatalog(cid int, level int, job int, rc robotRuntimeConfig) error {
	items := m.loadEquipmentCatalog()
	if len(items) == 0 {
		return nil
	}
	slots := rc.AvatarSlots
	if len(slots) == 0 {
		slots = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	}
	candidatesBySlot := make(map[int][]equipmentCatalogItem)
	for _, slot := range slots {
		itemType := slot + 20
		var candidates []equipmentCatalogItem
		for _, item := range items {
			if item.ID == 0 || item.Expire || item.ItemType != itemType {
				continue
			}
			if !avatarUsableByJob(item, job) {
				continue
			}
			candidates = append(candidates, item)
		}
		candidatesBySlot[slot] = candidates
	}
	selected := make(map[int]equipmentCatalogItem)
	if rc.PreferAvatarSets {
		selected = m.selectSetItems(candidatesBySlot, rc.AvatarSetMinSlots)
	}
	m.fillRandomItems(selected, candidatesBySlot)
	_, _ = m.db.Exec("DELETE FROM taiwan_cain_2nd.user_items WHERE charac_no=? AND slot<=9", cid)
	if rc.MinAvatarSlots > 0 && len(selected) < rc.MinAvatarSlots {
		return nil
	}
	for slot, item := range selected {
		_, _ = m.db.Exec("INSERT INTO taiwan_cain_2nd.user_items (charac_no,slot,it_id,expire_date,reg_date,obtain_from,hidden_option) VALUES (?,?,?,'9999-12-31 23:59:59',NOW(),0,1)", cid, slot, item.ID)
	}
	return nil
}

func (m *RobotManager) selectEquipment(items []equipmentCatalogItem, level int, job int, rc robotRuntimeConfig) map[int]equipmentCatalogItem {
	slots := rc.EquipSlots
	if len(slots) == 0 {
		slots = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	}
	candidatesBySlot := make(map[int][]equipmentCatalogItem)
	for _, slot := range slots {
		itemType := equipSlotToItemType(slot)
		if itemType == 0 {
			continue
		}
		var candidates []equipmentCatalogItem
		bestLevel := 0
		for _, item := range items {
			if item.ID == 0 || item.Expire || item.ItemType != itemType || item.Level > level {
				continue
			}
			if rc.EquipRarityMax > 0 && (item.Rarity < rc.EquipRarityMin || item.Rarity > rc.EquipRarityMax) {
				continue
			}
			if !itemUsableByJob(item.UseJob, job) {
				continue
			}
			if item.Level > bestLevel {
				bestLevel = item.Level
			}
			candidates = append(candidates, item)
		}
		if len(candidates) == 0 {
			continue
		}
		if bestLevel > 0 {
			var near []equipmentCatalogItem
			for _, item := range candidates {
				if item.Level >= bestLevel-10 {
					near = append(near, item)
				}
			}
			if len(near) > 0 {
				candidates = near
			}
		}
		candidatesBySlot[slot] = candidates
	}
	selected := make(map[int]equipmentCatalogItem)
	if rc.PreferEquipSets {
		selected = m.selectSetItems(candidatesBySlot, rc.EquipSetMinSlots)
	}
	m.fillRandomItems(selected, candidatesBySlot)
	return selected
}

func (m *RobotManager) selectSetItems(candidatesBySlot map[int][]equipmentCatalogItem, minSlots int) map[int]equipmentCatalogItem {
	if minSlots <= 1 {
		minSlots = 2
	}
	type setGroup struct {
		bySlot    map[int][]equipmentCatalogItem
		coverage  int
		levelSum  int
		raritySum int
		count     int
	}
	groups := make(map[string]*setGroup)
	for slot, candidates := range candidatesBySlot {
		for _, item := range candidates {
			if item.SetKey == "" {
				continue
			}
			group := groups[item.SetKey]
			if group == nil {
				group = &setGroup{bySlot: make(map[int][]equipmentCatalogItem)}
				groups[item.SetKey] = group
			}
			if len(group.bySlot[slot]) == 0 {
				group.coverage++
			}
			group.bySlot[slot] = append(group.bySlot[slot], item)
			group.levelSum += item.Level
			group.raritySum += item.Rarity
			group.count++
		}
	}
	var best []*setGroup
	bestScore := -1
	for _, group := range groups {
		if group.coverage < minSlots {
			continue
		}
		score := group.coverage*1000000 + safeAvg(group.levelSum, group.count)*1000 + safeAvg(group.raritySum, group.count)
		if score > bestScore {
			bestScore = score
			best = []*setGroup{group}
		} else if score == bestScore {
			best = append(best, group)
		}
	}
	selected := make(map[int]equipmentCatalogItem)
	if len(best) == 0 {
		return selected
	}
	group := best[m.randIntn(len(best))]
	for slot, candidates := range group.bySlot {
		if len(candidates) == 0 {
			continue
		}
		selected[slot] = candidates[m.randIntn(len(candidates))]
	}
	return selected
}

func (m *RobotManager) fillRandomItems(selected map[int]equipmentCatalogItem, candidatesBySlot map[int][]equipmentCatalogItem) {
	for slot, candidates := range candidatesBySlot {
		if _, ok := selected[slot]; ok || len(candidates) == 0 {
			continue
		}
		selected[slot] = candidates[m.randIntn(len(candidates))]
	}
}

func safeAvg(total, count int) int {
	if count <= 0 {
		return 0
	}
	return total / count
}

func (m *RobotManager) loadEquipmentCatalog() []equipmentCatalogItem {
	mod := configFileModFallback(m.cfg.ConfigDir, "pvf_equipment_catalog.json", "equipment_catalog.json")
	m.cacheMu.Lock()
	if m.equipCached && m.equipMod.Equal(mod) {
		items := m.equipCache
		m.cacheMu.Unlock()
		return items
	}
	m.cacheMu.Unlock()

	data, err := readConfigFileFallback(m.cfg.ConfigDir, "pvf_equipment_catalog.json", "equipment_catalog.json")
	if err != nil {
		return nil
	}
	var items []equipmentCatalogItem
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	m.cacheMu.Lock()
	m.equipCache = items
	m.equipMod = mod
	m.equipCached = true
	m.cacheMu.Unlock()
	return items
}

func (m *RobotManager) loadStackableCatalog() []equipmentCatalogItem {
	mod := configFileModFallback(m.cfg.ConfigDir, "pvf_stackable_catalog.json", "stackable_catalog.json")
	m.cacheMu.Lock()
	if m.stackCached && m.stackMod.Equal(mod) {
		items := m.stackCache
		m.cacheMu.Unlock()
		return items
	}
	m.cacheMu.Unlock()

	data, err := readConfigFileFallback(m.cfg.ConfigDir, "pvf_stackable_catalog.json", "stackable_catalog.json")
	if err != nil {
		return nil
	}
	var items []equipmentCatalogItem
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	m.cacheMu.Lock()
	m.stackCache = items
	m.stackMod = mod
	m.stackCached = true
	m.cacheMu.Unlock()
	return items
}

func equipSlotToItemType(slot int) int {
	switch slot {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 5
	case 6:
		return 6
	case 7:
		return 7
	case 8:
		return 8
	case 9:
		return 9
	case 10:
		return 10
	case 11:
		return 11
	case 12:
		return 12
	default:
		return 0
	}
}

func itemUsableByJob(jobs []int, job int) bool {
	if len(jobs) == 0 {
		return true
	}
	for _, j := range jobs {
		if j == 100 || j == job {
			return true
		}
	}
	return false
}

func avatarUsableByJob(item equipmentCatalogItem, job int) bool {
	if item.ItemType < 20 || item.ItemType > 29 {
		return false
	}
	if len(item.UseJob) == 0 {
		return item.ItemType == 29
	}
	for _, j := range item.UseJob {
		if j == job {
			return true
		}
	}
	return false
}

func writeEquipSlot(dst []byte, item equipmentCatalogItem, rng *rand.Rand, rc robotRuntimeConfig) {
	if len(dst) < 61 {
		return
	}
	dst[0] = 0x00
	dst[1] = 0x01
	binary.LittleEndian.PutUint32(dst[2:6], uint32(item.ID))
	intensifyMin := maxInt(rc.EquipIntensifyMin, 7)
	intensifyMax := maxInt(rc.EquipIntensifyMax, intensifyMin)
	intensify := randomBetween(rng, intensifyMin, intensifyMax)
	if item.ItemType == 1 {
		intensify = randomBetween(rng, 8, 15)
	}
	if item.ItemType == 2 {
		intensify = 0
	}
	dst[6] = byte(intensify)
	binary.LittleEndian.PutUint32(dst[7:11], uint32(randomBetween(rng, 0, 400000)))
	dst[11] = byte(randomBetween(rng, 10, 30))
	if item.ItemType == 1 {
		dst[51] = byte(randomBetween(rng, rc.EquipSmithingMin, rc.EquipSmithingMax))
	}
}

func compressRaw(raw []byte) []byte {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	blob := append(make([]byte, 4), compressed.Bytes()...)
	binary.LittleEndian.PutUint32(blob[0:4], uint32(len(raw)))
	return blob
}
