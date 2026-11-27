package dnf

import (
	"encoding/binary"
	"time"

	foundationlog "robot/internal/foundation/log"
)

// Resources use the server's base character pool for simple combat simulation.
type characterResourcePool struct {
	known        bool
	hp, mp       int32
	maxHP, maxMP int32
	recoveredAt  time.Time
	lastHurtAt   time.Time
	syncPending  bool
}

func (r *RobotVo) initializeCharacterResourcesUnsafe() {
	info := r.characterBaseInfo
	if r.characterResources.known || !info.known {
		return
	}
	hp := max(1, int32(binary.LittleEndian.Uint32(info.stats[0:4])/10))
	mp := max(1, int32(info.baseMP()))
	r.characterResources = characterResourcePool{known: true, hp: hp, mp: mp, maxHP: hp, maxMP: mp}
	foundationlog.Robotf("[CHARACTER_RESOURCE_INIT] uid=%d cid=%d source=simulation_base hp=%d mp=%d\n", r.UID, r.CID, hp, mp)
}

func (r *RobotVo) seedPartyResourcesUnsafe() {
	r.initializeCharacterResourcesUnsafe()
	if !r.characterResources.known || r.partySelfPeer.uniqueID == 0 {
		return
	}
	if r.partyDungeonWorld.actors == nil {
		r.partyDungeonWorld.actors = make(map[[4]byte]partyDungeonActor)
	}
	ref := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[ref]
	if !actor.hpKnown {
		actor.hp, actor.hpKnown = r.characterResources.hp, true
	}
	if !actor.mpKnown {
		actor.mp, actor.mpKnown = r.characterResources.mp, true
	}
	r.partyDungeonWorld.actors[ref] = actor
}

func (r *RobotVo) rememberPartyResourcesUnsafe() {
	actor := r.partyDungeonWorld.actors[partyDungeonPlayerReference(r.partySelfPeer.uniqueID)]
	if actor.hpKnown {
		r.characterResources.hp = actor.hp
	}
	if actor.mpKnown {
		r.characterResources.mp = actor.mp
	}
}
