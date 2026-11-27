package dnf

import (
	"encoding/binary"

	"robot/internal/shared"
)

func (r *RobotVo) SetArea(village, area uint8, x, y uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, uint16(r.CurVillage), uint16(r.CurArea))
}

func (r *RobotVo) SetAreaFrom(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, fromVillage, fromArea)
}

func (r *RobotVo) setAreaFromLocked(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	if !shared.GenericAreaAllowed(r.GuildID, int(village)) {
		return
	}
	areaChanged := r.CurVillage != village || r.CurArea != area
	setArea := r.setArea
	setArea[0] = village
	setArea[1] = area
	binary.LittleEndian.PutUint16(setArea[2:4], x)
	binary.LittleEndian.PutUint16(setArea[4:6], y)
	// The reference reads the origin pair right after the z byte: previous
	// village at offset 7 and previous area at offset 9, both as shorts.
	binary.LittleEndian.PutUint16(setArea[7:9], fromVillage)
	binary.LittleEndian.PutUint16(setArea[9:11], fromArea)

	pkt, err := buildSendPacket(38, uint16(r.PacketID), setArea[:], r.Cipher)
	r.PacketID++
	if err == nil {
		if r.SendMsg(pkt) {
			if areaChanged {
				r.townEntityPositions = make(map[uint16]townEntityPosition)
			}
			r.CurVillage = village
			r.CurArea = area
			r.CurX = x
			r.CurY = y
		}
	}
}

func (r *RobotVo) SetPosition(x, y uint16, typ uint8, speed uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	r.setPositionUnsafe(x, y, typ, speed)
}

func (r *RobotVo) setPositionUnsafe(x, y uint16, typ uint8, speed uint16) bool {
	var setPos [8]byte
	setPos[0] = 0xDA
	setPos[1] = 0x01
	setPos[2] = 0xEA
	setPos[4] = 0x05
	setPos[5] = 0x64

	binary.LittleEndian.PutUint16(setPos[0:2], x)
	binary.LittleEndian.PutUint16(setPos[2:4], y)
	setPos[4] = typ
	binary.LittleEndian.PutUint16(setPos[5:7], speed)

	pkt, err := buildSendPacket(37, uint16(r.PacketID), setPos[:], r.Cipher)
	r.PacketID++
	if err != nil || !r.SendMsg(pkt) {
		return false
	}
	r.CurX = x
	r.CurY = y
	r.MoveType = typ
	return true
}
