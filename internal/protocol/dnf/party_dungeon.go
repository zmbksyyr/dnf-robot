package dnf

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

const (
	partyDungeonActivityTimeout = 15 * time.Second
	partySkillRecoveryGrace     = 2 * time.Second
)

func (r *RobotVo) shouldFollowPartyPeerUnsafe(peer partyIPPeer) bool {
	return r.partySelfPeer.slotKnown && r.partySelfPeer.slot != 0 && peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0
}

func (r *RobotVo) tracePartyDungeonFrameUnsafe(frame []byte, route byte, peer partyIPPeer) {
	now := time.Now()
	if now.Before(r.partyDungeonTraceAt) || len(frame) < 9 {
		return
	}
	r.partyDungeonTraceAt = now.Add(2 * time.Second)
	bodySize := binary.LittleEndian.Uint16(frame[5:7])
	follow := false
	if frame[0] == 0x02 {
		_, _, follow = rewritePartyDungeonBody(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
	}
	foundationlog.Robotf("[PARTY_DUNGEON_FRAME] uid=%d route=%d peer_slot=%d peer_unique_id=%d type=%d body=%d records=%s follow=%t\n", r.UID, route, peer.slot, peer.uniqueID, frame[0], bodySize, partyDungeonFrameRecords(frame), follow)
}

func (r *RobotVo) queuePartyDungeonFollowUnsafe(frame []byte, peer partyIPPeer, now time.Time) bool {
	if len(frame) < 9 || !peer.slotKnown || peer.slot >= 4 {
		return false
	}
	bodySize := int(binary.LittleEndian.Uint16(frame[5:7]))
	if len(frame) != 9+bodySize {
		return false
	}
	pending := partyDungeonFollowPending{
		due:      now.Add(r.partyDungeonFollowDelayUnsafe()),
		peerSlot: peer.slot,
		flags:    frame[8],
	}
	switch frame[0] {
	case 0x02:
		body, _, ok := rewritePartyDungeonBody(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if !ok {
			return false
		}
		pending.body = body
	case 0x01:
		records := rewritePartyDungeonRecords(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if len(records) == 0 {
			return false
		}
		pending.reliable = true
		pending.records = records
	default:
		return false
	}
	if len(r.partyDungeonFollow) >= 2048 {
		r.partyDungeonFollow = nil
		return false
	}
	r.partyDungeonFollow = append(r.partyDungeonFollow, pending)
	return true
}

func (r *RobotVo) partyDungeonFollowDelayUnsafe() time.Duration {
	return time.Duration(2000+int(r.UID%2001)) * time.Millisecond
}

func (r *RobotVo) rememberPartyDungeonActivityUnsafe(frame []byte, route byte, peer partyIPPeer, now time.Time) {
	if (route != 1 && route != 2) || !r.shouldFollowPartyPeerUnsafe(peer) || len(frame) < 9 {
		return
	}
	if !partyDungeonFrameContainsCommand(frame, 0x0004) && !partyDungeonFrameContainsCommand(frame, 0x0027) && !partyDungeonFrameContainsCommand(frame, 0x0051) {
		return
	}
	r.partyDungeonLastAt = now
	r.partyDungeonFlags = frame[8]
	if r.partySkillNextAt.IsZero() {
		r.partySkillNextAt = now.Add(partySkillDelay(r.UID, now))
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_SCHEDULE] uid=%d due_in=%s type=%d records=%s\n", r.UID, r.partySkillNextAt.Sub(now), frame[0], partyDungeonFrameRecords(frame))
	}
}

func (r *RobotVo) flushPartyDungeonSkillUnsafe(conn *net.UDPConn, now time.Time) {
	if r.partyDungeonLastAt.IsZero() {
		if !r.partySkillNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_EXPIRED] uid=%d idle=%s due_in=%s recover_in=%s\n", r.UID, now.Sub(r.partyDungeonLastAt), r.partySkillNextAt.Sub(now), r.partySkillRecoverAt.Sub(now))
		}
		r.partySkillNextAt = time.Time{}
		r.partySkillRecoverAt = time.Time{}
		return
	}
	if !r.partySkillRecoverAt.IsZero() && !now.Before(r.partySkillRecoverAt) {
		if r.sendPartySkillStateUnsafe(conn, now, 0, nil, "RECOVER") {
			r.partySkillRecoverAt = time.Time{}
		}
	}
	idle := now.Sub(r.partyDungeonLastAt)
	if idle > partyDungeonActivityTimeout {
		if !r.partySkillNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_EXPIRED] uid=%d idle=%s due_in=%s recover_in=%s\n", r.UID, idle, r.partySkillNextAt.Sub(now), r.partySkillRecoverAt.Sub(now))
		}
		r.partySkillNextAt = time.Time{}
		if idle > partyDungeonActivityTimeout+partySkillRecoveryGrace {
			r.partySkillRecoverAt = time.Time{}
		}
		return
	}
	if r.partySkillNextAt.IsZero() || now.Before(r.partySkillNextAt) {
		return
	}
	r.partySkillNextAt = now.Add(partySkillDelay(r.UID, now))
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_DUE] uid=%d idle=%s\n", r.UID, now.Sub(r.partyDungeonLastAt))
	if !r.ensurePartySkillProfileUnsafe() || len(r.partySkillCandidates) == 0 {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 {
		return
	}
	candidate := r.nextPartySkillCandidateUnsafe(now)
	if r.sendPartySkillStateUnsafe(conn, now, candidate.state, candidate.stateData, "CAST") {
		r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL] uid=%d job=%d skill=%d state=%d level=%d name=%s data=%x risk=%d path=%s recover_in=%s\n", r.UID, r.partySkillJob, candidate.skillIndex, candidate.state, candidate.level, candidate.name, candidate.stateData, candidate.risk, candidate.path, r.partySkillRecoverAt.Sub(now))
	}
}

func (r *RobotVo) nextPartySkillCandidateUnsafe(now time.Time) partySkillCandidate {
	if len(r.partySkillCandidates) == 0 {
		return partySkillCandidate{}
	}
	return r.partySkillCandidates[partySkillChoice(r.UID, now, len(r.partySkillCandidates))]
}

func (r *RobotVo) sendPartySkillStateUnsafe(conn *net.UDPConn, now time.Time, state byte, stateData []byte, reason string) bool {
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 {
		return false
	}
	body := buildPartySkillStateBody(r.partySelfPeer.uniqueID, state, stateData, partySkillToken(r.UID, now))
	route := r.partyRouteForPeerUnsafe(peer.slot)
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, true)
	if !ok {
		return false
	}
	payload := buildPartyReliablePacket(sequence, r.partySelfPeer.slot, r.partyDungeonFlags, [][]byte{body})
	destination, err := r.sendPartyTransportUnsafe(conn, peer, route, payload)
	if err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s_ERROR] uid=%d state=%d data=%x route=%d destination=%s err=%v\n", reason, r.UID, state, stateData, route, destination, err)
		return false
	}
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s] uid=%d state=%d data=%x sequence=%d route=%d destination=%s\n", reason, r.UID, state, stateData, sequence, route, destination)
	return true
}

func (r *RobotVo) ensurePartySkillProfileUnsafe() bool {
	if r.partySkillLoaded {
		return true
	}
	if r.partySkillLoading {
		return false
	}
	db := r.DB
	if db == nil {
		db = GetDBPool()
	}
	loader := r.partySkillLoad
	if (loader == nil && db == nil) || r.UID == 0 {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_ERROR] uid=%d cid=%d db_ready=%t\n", r.UID, r.CID, db != nil)
		return false
	}
	if loader == nil {
		loader = func(uid uint32, cid int) (partySkillProfile, error) {
			return queryPartySkillProfile(db, uid, cid)
		}
	}
	r.partySkillLoading = true
	generation := r.partySkillGeneration
	uid := r.UID
	cid := r.CID
	go r.loadPartySkillProfile(generation, uid, cid, loader)
	return false
}

func (r *RobotVo) loadPartySkillProfile(generation uint64, uid uint32, cid int, loader partySkillProfileLoadFunc) {
	profile, err := loader(uid, cid)
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.partySkillGeneration || r.UID != uid || r.State == StateStop {
		return
	}
	r.partySkillLoading = false
	if err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_ERROR] uid=%d cid=%d err=%v\n", uid, cid, err)
		return
	}
	if r.CID == 0 {
		r.CID = profile.cid
	}
	r.partySkillLoaded = true
	r.partySkillJob = profile.job
	r.partySkillCandidates = profile.candidates
	if !r.partyDungeonLastAt.IsZero() && now.Sub(r.partyDungeonLastAt) <= partyDungeonActivityTimeout {
		r.partySkillNextAt = now
	}
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE] uid=%d cid=%d job=%d whitelist=%d pvf=%d pvf_matched=%d candidates=%d skipped_missing_pvf=%d skipped_path_mismatch=%d\n", uid, profile.cid, profile.job, profile.whitelistCount, profile.pvfCount, profile.stats.PVFMatched, len(profile.candidates), profile.stats.SkippedMissingPVF, profile.stats.SkippedPathMismatch)
}

func (r *RobotVo) resetPartySkillProfileUnsafe() {
	r.partySkillGeneration++
	r.partySkillLoaded = false
	r.partySkillLoading = false
	r.partySkillJob = 0
	r.partySkillCandidates = nil
}

func (r *RobotVo) flushPartyDungeonFollowUnsafe(conn *net.UDPConn, now time.Time) {
	for len(r.partyDungeonFollow) > 0 && !now.Before(r.partyDungeonFollow[0].due) {
		pending := r.partyDungeonFollow[0]
		r.partyDungeonFollow = r.partyDungeonFollow[1:]
		peer := r.partyPeerForSlotUnsafe(pending.peerSlot)
		if peer.uniqueID == 0 {
			continue
		}
		route := r.partyRouteForPeerUnsafe(peer.slot)
		sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, pending.reliable)
		if !ok {
			continue
		}
		var payload []byte
		if pending.reliable {
			payload = buildPartyReliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.records)
		} else {
			payload = buildPartyUnreliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.body)
		}
		if _, err := r.sendPartyTransportUnsafe(conn, peer, route, payload); err != nil {
			foundationlog.Robotf("[PARTY_DUNGEON_FOLLOW_ERROR] uid=%d peer=%d route=%d err=%v\n", r.UID, peer.accID, route, err)
		}
	}
}

func partyDungeonFrameContainsCommand(frame []byte, target uint16) bool {
	if len(frame) < 12 {
		return false
	}
	if frame[0] == 0x02 {
		return binary.LittleEndian.Uint16(frame[10:12]) == target
	}
	if frame[0] != 0x01 {
		return false
	}
	body := frame[9:]
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return false
		}
		if size >= 3 && binary.LittleEndian.Uint16(body[1:3]) == target {
			return true
		}
		body = body[size:]
	}
	return false
}

func partyDungeonFrameRecords(frame []byte) string {
	if len(frame) < 9 {
		return "invalid"
	}
	if frame[0] == 0x02 {
		if len(frame) < 12 {
			return "short"
		}
		return fmt.Sprintf("0x%04x/%d", binary.LittleEndian.Uint16(frame[10:12]), len(frame)-9)
	}
	if frame[0] != 0x01 {
		return "control"
	}
	body := frame[9:]
	records := ""
	for count := 0; len(body) >= 2 && count < 8; count++ {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return records + "invalid"
		}
		command := uint16(0)
		if size >= 3 {
			command = binary.LittleEndian.Uint16(body[1:3])
		}
		if records != "" {
			records += ","
		}
		records += fmt.Sprintf("0x%04x/%d", command, size)
		body = body[size:]
	}
	if records == "" {
		return "empty"
	}
	return records
}

type partyDungeonFollowPending struct {
	due      time.Time
	peerSlot byte
	flags    byte
	reliable bool
	body     []byte
	records  [][]byte
}

type partySkillCandidate struct {
	skillIndex byte
	state      byte
	level      int
	name       string
	stateData  []byte
	risk       int
	path       string
}

type partySkillProfile struct {
	cid            int
	job            int
	whitelistCount int
	pvfCount       int
	candidates     []partySkillCandidate
	stats          partySkillCandidateStats
}

type partySkillProfileLoadFunc func(uid uint32, cid int) (partySkillProfile, error)

type partySkillCandidateStats struct {
	PVFMatched          int
	SkippedMissingPVF   int
	SkippedPathMismatch int
}

func queryPartySkillProfile(db *sql.DB, uid uint32, cid int) (partySkillProfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var profile partySkillProfile
	if err := db.QueryRowContext(ctx, "SELECT c.charac_no,c.job FROM taiwan_cain.charac_info c WHERE c.m_id=? AND (?=0 OR c.charac_no=?) AND c.delete_flag=0 ORDER BY c.charac_no LIMIT 1", uid, cid, cid).Scan(&profile.cid, &profile.job); err != nil {
		return partySkillProfile{}, err
	}
	whitelist := shared.PartySkillStatesForJob(profile.job)
	pvfStates := shared.SkillStatesForJob(profile.job)
	profile.whitelistCount = len(whitelist)
	profile.pvfCount = len(pvfStates)
	profile.candidates, profile.stats = partySkillCandidatesFromCatalog(profile.job, whitelist, pvfStates)
	return profile, nil
}

func partySkillCandidatesFromCatalog(job int, whitelist []shared.PartySkillState, pvfStates []shared.SkillState) ([]partySkillCandidate, partySkillCandidateStats) {
	pvfIndex := make(map[[3]int]map[string]struct{}, len(pvfStates))
	for _, entry := range pvfStates {
		if entry.Job != job || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		key := [3]int{entry.Job, entry.SkillIndex, entry.State}
		paths := pvfIndex[key]
		if paths == nil {
			paths = make(map[string]struct{})
			pvfIndex[key] = paths
		}
		paths[normalizeSkillScriptPath(entry.ScriptPath)] = struct{}{}
	}
	candidates := make([]partySkillCandidate, 0, len(whitelist))
	stats := partySkillCandidateStats{}
	for _, entry := range whitelist {
		if entry.Job != job || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		paths, ok := pvfIndex[[3]int{entry.Job, entry.SkillIndex, entry.State}]
		if !ok {
			stats.SkippedMissingPVF++
			continue
		}
		if scriptPath := normalizeSkillScriptPath(entry.ScriptPath); scriptPath != "" {
			if _, ok := paths[scriptPath]; !ok {
				stats.SkippedPathMismatch++
				continue
			}
		}
		stats.PVFMatched++
		candidates = append(candidates, partySkillCandidate{
			skillIndex: byte(entry.SkillIndex),
			state:      byte(entry.State),
			level:      entry.Level,
			name:       entry.Name,
			stateData:  append([]byte(nil), entry.StateData...),
			risk:       entry.Risk,
			path:       entry.ScriptPath,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].risk != candidates[j].risk {
			return candidates[i].risk < candidates[j].risk
		}
		if candidates[i].skillIndex != candidates[j].skillIndex {
			return candidates[i].skillIndex < candidates[j].skillIndex
		}
		return candidates[i].state < candidates[j].state
	})
	return candidates, stats
}

func normalizeSkillScriptPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.Trim(value, "/")
	return strings.ToLower(value)
}

const partyDungeonStateCommand = 0x0004

const partyDungeonEnvelopeCommand = 0x0051

const partyDungeonEnvelopeMinBodySize = 26

const partyDungeonEnvelopePayloadOffset = 22

const partySkillStateBodyBaseSize = 31

const partySkillRecoverDelay = 900 * time.Millisecond

var partyDungeonEnvelopeChecksumOffsets = [...]int{10, 18}

func buildPartySkillStateBody(uniqueID uint16, state byte, stateData []byte, token uint16) []byte {
	body := make([]byte, partySkillStateBodyBaseSize+len(stateData))
	body[0] = 0x02
	binary.LittleEndian.PutUint16(body[1:3], partyDungeonEnvelopeCommand)
	body[7] = 0x02
	body[8] = 0x05
	body[14] = 0x00
	body[15] = 0x02
	body[16] = 0x05
	payload := body[partyDungeonEnvelopePayloadOffset:]
	payload[0] = 0x11
	payload[1] = 0x01
	binary.LittleEndian.PutUint16(payload[2:4], uniqueID)
	payload[4] = state
	binary.LittleEndian.PutUint16(payload[5:7], uint16(len(stateData)))
	copy(payload[7:], stateData)
	binary.LittleEndian.PutUint16(payload[7+len(stateData):9+len(stateData)], token)
	innerChecksum := partyPayloadChecksum(payload)
	for _, offset := range partyDungeonEnvelopeChecksumOffsets {
		copy(body[offset:offset+len(innerChecksum)], innerChecksum[:])
	}
	outerChecksum := partyPayloadChecksum(body[7:])
	body[3] = outerChecksum[0]
	body[4] = byte(token)
	body[5] = byte(token >> 8)
	body[6] = byte(uniqueID>>8) ^ state ^ byte(len(stateData))
	return body
}

func partySkillDelay(uid uint32, now time.Time) time.Duration {
	return time.Duration(4+partySkillChoice(uid, now, 6)) * time.Second
}

func partySkillChoice(uid uint32, now time.Time, count int) int {
	if count <= 1 {
		return 0
	}
	value := uint64(now.UnixNano()) ^ uint64(uid)*0x9e3779b97f4a7c15
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	return int(value % uint64(count))
}

func partySkillToken(uid uint32, now time.Time) uint16 {
	value := uint64(now.UnixNano()) ^ uint64(uid)<<32
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	return uint16(value)
}

func rewritePartyDungeonBody(body []byte, sourceUniqueID, targetUniqueID uint16) ([]byte, uint16, bool) {
	if len(body) < 7 || body[0] != 1 || sourceUniqueID == 0 || targetUniqueID == 0 || sourceUniqueID == targetUniqueID {
		return nil, 0, false
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	followBody := append([]byte(nil), body...)
	var checksum [4]byte
	switch command {
	case partyDungeonEnvelopeCommand:
		if len(body) < partyDungeonEnvelopeMinBodySize {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(body[7:])
		if !bytes.Equal(body[3:7], checksum[:]) {
			return nil, 0, false
		}
		innerChecksum := partyPayloadChecksum(body[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			if !bytes.Equal(body[offset:offset+len(innerChecksum)], innerChecksum[:]) {
				return nil, 0, false
			}
		}
		if rewritePartyDungeonEnvelopeIdentity(followBody[partyDungeonEnvelopePayloadOffset:], sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		innerChecksum = partyPayloadChecksum(followBody[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			copy(followBody[offset:offset+len(innerChecksum)], innerChecksum[:])
		}
	case partyDungeonStateCommand:
		plain := followBody[7:]
		for i := range plain {
			plain[i] ^= 0x7e
		}
		checksum = partyPayloadChecksum(plain)
		if !bytes.Equal(body[3:7], checksum[:]) || rewritePartyDungeonStateIdentity(plain, sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(plain)
		copy(followBody[3:7], checksum[:])
		for i := range plain {
			plain[i] ^= 0x7e
		}
	default:
		return nil, 0, false
	}
	if command == partyDungeonEnvelopeCommand {
		checksum = partyPayloadChecksum(followBody[7:])
		copy(followBody[3:7], checksum[:])
	}
	return followBody, command, true
}

func rewritePartyDungeonRecords(body []byte, sourceUniqueID, targetUniqueID uint16) [][]byte {
	records := make([][]byte, 0, 2)
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return nil
		}
		if record, _, ok := rewritePartyDungeonBody(body[:size], sourceUniqueID, targetUniqueID); ok {
			records = append(records, record)
		}
		body = body[size:]
	}
	if len(body) != 0 {
		return nil
	}
	return records
}

func rewritePartyDungeonEnvelopeIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	if len(payload) < 6 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{2, 4} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
}

func rewritePartyDungeonStateIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	const singleStateHeaderSize = 15
	if len(payload) < singleStateHeaderSize || payload[0] != 1 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{3, 7} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
}

func buildFinishLoadingPayload(inventoryChecksum, skillChecksum uint32) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[:4], inventoryChecksum)
	binary.LittleEndian.PutUint32(body[4:], skillChecksum)
	return body
}
