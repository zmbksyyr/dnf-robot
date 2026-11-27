package repository

import (
	"fmt"

	equipcap "robot/internal/capability/equipment"
	"robot/internal/shared"
)

func maxRobotSkillSlots(job, grow int) ([]byte, error) {
	learned := shared.MaxLearnedSkillsForJob(job, grow)
	if len(learned) == 0 || len(learned) > 204 {
		return nil, fmt.Errorf("PVF learned skills unavailable or exceed 204 slots: job=%d grow=%d count=%d", job, grow, len(learned))
	}
	// Native skill format 3 stores 204 index/level pairs behind a length and
	// zlib header. Both skill trees start with the same fully learned profile.
	raw := make([]byte, 408)
	for i, skill := range learned {
		raw[i*2], raw[i*2+1] = skill.Index, skill.Level
	}
	return equipcap.CompressRaw(raw), nil
}
