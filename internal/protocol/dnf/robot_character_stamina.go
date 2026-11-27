package dnf

import foundationlog "robot/internal/foundation/log"

// Stamina notifications have one logical byte. Encrypted server bodies also
// contain a four-byte trailer and zero padding to the type's cipher block size:
// Twofish/16 for notification 4, TEA/8 for notification 33. The trailer is not
// padding. Checking only the first byte could accept ciphertext as plain data.
func parseCharacterStamina(body []byte, typ uint16) (byte, bool) {
	blockSize := 16
	if typ == 33 {
		blockSize = 8
	} else if typ != 4 {
		return 0, false
	}
	if len(body) != 1 && len(body) != blockSize {
		return 0, false
	}
	if body[0] > 100 {
		return 0, false
	}
	if len(body) > 1 {
		for _, value := range body[5:] {
			if value != 0 {
				return 0, false
			}
		}
	}
	return body[0], true
}

func (r *RobotVo) handleCharacterStaminaUnsafe(packet robotInboundPacket) {
	if packet.flag != 0 || (packet.typ != 4 && packet.typ != 33) ||
		(r.State != StateLogin && r.State != StateRun) || r.ReturnSelectPending ||
		!r.characterBaseInfo.known || r.characterUniqueID == 0 || r.characterBaseInfo.uniqueID != r.characterUniqueID {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	var stamina byte
	known := false
	for _, candidate := range candidates {
		value, ok := parseCharacterStamina(candidate.body, packet.typ)
		if !ok {
			continue
		}
		if known && stamina != value {
			return
		}
		stamina, known = value, true
	}
	if known && stamina != r.characterBaseInfo.stamina {
		r.characterBaseInfo.stamina = stamina
		foundationlog.Robotf("[CHARACTER_STAMINA] uid=%d cid=%d unique=%d type=%d stamina=%d\n", r.UID, r.CID, r.characterUniqueID, packet.typ, stamina)
	}
}
