package dnf

// ReturnToCharacterSelect performs the game's normal "small logout". Unlike
// CloseOut it preserves the authenticated account connection. The successful
// server response is the write barrier: ReturnToSelectCharacList() has already
// saved UpdateData(), unloaded the cached character and reset the current
// character before sending that response.
func (r *RobotVo) ReturnToCharacterSelect() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateRun || r.ReturnSelectPending || r.partyActiveUnsafe() {
		return false
	}
	pkt, err := buildSendPacket(7, uint16(r.PacketID), nil, r.Cipher)
	if err != nil || !r.sendRaw(pkt) {
		return false
	}
	r.PacketID++
	r.ReturnSelectPending = true
	r.ReturnSelectRejected = false
	r.publishSnapshotUnsafe()
	return true
}

// ReselectCharacter enters the same configured character from StateSelect.
// The normal type-300 login completion packet moves the robot back to StateRun.
func (r *RobotVo) ReselectCharacter() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateSelect || r.ReturnSelectPending || r.ReturnSelectRejected {
		return false
	}
	r.State = StateLogin
	r.SelectCharacSent = false
	if !r.sendSelectCharacUnsafe("after character refresh") {
		r.State = StateSelect
		r.publishSnapshotUnsafe()
		return false
	}
	r.publishSnapshotUnsafe()
	return true
}

// handleReturnToSelectPacketUnsafe consumes only the response to our own
// pending command 7. Other type-7 packets remain available to the party/trade
// peer-request handler.
func (r *RobotVo) handleReturnToSelectPacketUnsafe(packet robotInboundPacket) bool {
	if packet.typ != 7 || !r.ReturnSelectPending {
		return false
	}
	// This df_game family acknowledges ReturnToSelectCharacter with a flag=1,
	// type=7 packet and no response body. Some sibling builds use the ordinary
	// flag=0 response containing a one-byte success value, handled below.
	if packet.flag == 1 {
		r.finishReturnToSelectUnsafe()
		return true
	}
	if packet.flag != 0 {
		return false
	}
	candidates, err := recvBodyCandidates(r.Cipher, packet.data, packet.isAnti)
	if err != nil && len(candidates) == 0 {
		return true
	}
	for _, candidate := range candidates {
		if len(candidate.body) < 1 || (candidate.body[0] != 0 && candidate.body[0] != 1) {
			continue
		}
		r.ReturnSelectPending = false
		if candidate.body[0] != 1 {
			r.ReturnSelectRejected = true
			r.publishSnapshotUnsafe()
			return true
		}
		r.finishReturnToSelectUnsafe()
		return true
	}
	return true
}

func (r *RobotVo) finishReturnToSelectUnsafe() {
	r.ReturnSelectPending = false
	r.ReturnSelectRejected = false
	r.SelectCharacSent = false
	r.State = StateSelect
	r.stopPartySupervisorUnsafe()
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	r.clearPartyPendingUnsafe()
	r.publishSnapshotUnsafe()
}
