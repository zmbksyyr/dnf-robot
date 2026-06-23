package service

import (
	"os"
	"time"
)

func fileModTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

func cloneRobotRuntimeConfig(rc robotRuntimeConfig) robotRuntimeConfig {
	rc.Jobs = append([]int(nil), rc.Jobs...)
	rc.GrowTypes = append([]int(nil), rc.GrowTypes...)
	rc.EquipSlots = append([]int(nil), rc.EquipSlots...)
	rc.AvatarSlots = append([]int(nil), rc.AvatarSlots...)
	rc.StoreItemAllowIDs = append([]int(nil), rc.StoreItemAllowIDs...)
	rc.StoreItemDenyIDs = append([]int(nil), rc.StoreItemDenyIDs...)
	return rc
}

func (m *RobotManager) invalidateRobotConfigCache() {
	m.cacheMu.Lock()
	m.configCached = false
	m.cacheMu.Unlock()
}
