package dnf

import "encoding/binary"

// CMD18 success carries deleted quantities, without item identities or a request
// token. Failure may follow a partially applied request, including deletions not
// covered by the successful prefix. Until outgoing requests are correlated, an
// acknowledgement cannot safely be subtracted from the current inventory: its
// value may be an equipment instance ID or an already updated stack quantity.
func (r *RobotVo) handleInventoryDeleteReplyUnsafe(packet robotInboundPacket) {
	if packet.flag == 0 && !r.ReturnSelectPending && (r.State == StateRun || r.State == StateLogin) {
		// NOTI18 moves items independently of a CMD18 deletion reply.
		r.inventoryChecksumKnown = false
		r.characterBaseInfo.equipment.known = false
		return
	}
	if packet.flag != 1 || r.ReturnSelectPending || r.State != StateRun {
		return
	}
	candidates, _ := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	for _, candidate := range candidates {
		space, changed, ok := parseInventoryDeleteReply(candidate.body)
		if ok && space == 0 && changed {
			// Any plausible main-space mutation makes the baseline unavailable.
			// Do not request CMD20 here: that command sorts and relocates items.
			r.InfanMap = nil
			r.inventoryChecksumRecords = nil
			r.inventoryChecksumKnown = false
			r.inventoryNeedsSnapshot = true
			r.storeDisplayCandidates = nil
			r.storeDisplayRetryPlans = nil
			r.storeDisplayRetryIndex = 0
			r.storeInventoryVersion++
			return
		}
	}
}

func parseInventoryDeleteReply(body []byte) (space byte, changed, ok bool) {
	if len(body) < 3 {
		return 0, false, false
	}
	end := 3
	switch body[0] {
	case 0:
		// Error is before item space; no per-slot progress is carried here.
		space, changed = body[2], true
	case 1:
		space = body[1]
		count := int(body[2])
		if count > 100 || count > (len(body)-3)/8 {
			return 0, false, false
		}
		end += 8 * count
		for pos := 3; pos < end; pos += 8 {
			slot := int(int16(binary.LittleEndian.Uint16(body[pos:])))
			quantity := int32(binary.LittleEndian.Uint32(body[pos+2:]))
			if quantity <= 0 || space == 0 && (slot < 0 || slot >= characterInventorySlots) {
				return 0, false, false
			}
		}
		changed = count != 0
	default:
		return 0, false, false
	}
	// Original replies carry a four-byte hash and Twofish alignment. Plain/anti
	// envelopes may omit them. Alignment bytes need not be zero.
	if len(body) != end && len(body) != end+4 && len(body) != alignTo(end+4, 16) {
		return 0, false, false
	}
	return space, changed, true
}
