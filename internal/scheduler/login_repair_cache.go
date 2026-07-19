package scheduler

type loginRepairInvalidator interface {
	InvalidateLoginRepairs(uids []int)
}

func (m *RobotManager) invalidateLoginRepairs(uids []int) {
	if m == nil || len(uids) == 0 {
		return
	}
	if invalidator, ok := m.doll.(loginRepairInvalidator); ok {
		invalidator.InvalidateLoginRepairs(append([]int(nil), uids...))
	}
}
