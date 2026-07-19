package dnf

import (
	"context"
	"database/sql"
	"encoding/binary"
	"net"
	"sort"
	"time"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

const (
	partyDungeonActivityTimeout = 15 * time.Second
	partyDungeonEntryTimeout    = 30 * time.Minute
	partySkillRecoveryGrace     = 2 * time.Second
	partySkillRecoveryRetry     = 750 * time.Millisecond
	partySkillRecoverDelay      = 900 * time.Millisecond
	partySkillFailureCooldown   = 30 * time.Second
	partySkillStateBodyBaseSize = 31
)

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

type partySkillCandidateStats = shared.PartySkillMatchStats

func (r *RobotVo) rememberPartyDungeonActivityUnsafe(frame []byte, route byte, peer partyIPPeer, now time.Time) {
	if (route != 1 && route != 2) || !r.shouldFollowPartyPeerUnsafe(peer) || len(frame) < 9 {
		return
	}
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout {
		return
	}
	strongActivity := partyDungeonFrameContainsCommand(frame, 0x0004) || partyDungeonFrameContainsCommand(frame, 0x0027)
	if !strongActivity {
		if !partyDungeonFrameContainsCommand(frame, 0x0051) || r.partyDungeonLastAt.IsZero() || now.Sub(r.partyDungeonLastAt) > partyDungeonActivityTimeout {
			return
		}
	}
	r.partyDungeonLastAt = now
	r.partyDungeonFlags = frame[8]
	if r.partySkillNextAt.IsZero() {
		r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
	}
}

func (r *RobotVo) markPartyDungeonEnteredUnsafe(now time.Time) {
	r.partyDungeonEnteredAt = now
	if r.partyDungeonLastAt.IsZero() {
		r.partyDungeonLastAt = now
	}
	if r.partySkillNextAt.IsZero() {
		r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
	}
}

func (r *RobotVo) clearPartyDungeonRuntimeUnsafe() {
	if r.partyDungeonEnteredAt.IsZero() && r.partyDungeonLastAt.IsZero() && r.partySkillNextAt.IsZero() && r.partySkillRecoverAt.IsZero() && len(r.partyDungeonFollow) == 0 {
		return
	}
	r.cancelPartyDungeonReliableUnsafe(time.Now())
	r.partyDungeonFollow = nil
	r.partyDungeonEnteredAt = time.Time{}
	r.partyDungeonLastAt = time.Time{}
	r.partyDungeonFlags = 0
	r.partySkillNextAt = time.Time{}
	r.partySkillRecoverAt = time.Time{}
	r.partySkillBlockedUntil = time.Time{}
}

func (r *RobotVo) flushPartyDungeonSkillUnsafe(conn *net.UDPConn, now time.Time) {
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout {
		r.clearPartyDungeonRuntimeUnsafe()
		return
	}
	if r.partyDungeonLastAt.IsZero() {
		r.partySkillNextAt = time.Time{}
		r.partySkillRecoverAt = time.Time{}
		return
	}
	if !r.partySkillRecoverAt.IsZero() && !now.Before(r.partySkillRecoverAt) {
		peer := r.partyPeerForSlotUnsafe(0)
		if peer.slotKnown && r.partyReliablePurposePendingUnsafe(peer.slot, "skill-cast") {
			r.partySkillRecoverAt = now.Add(partySkillRecoveryRetry)
			return
		}
		if r.sendPartySkillStateUnsafe(conn, now, 0, nil, "RECOVER") {
			r.partySkillRecoverAt = time.Time{}
			if !r.partySkillNextAt.IsZero() && !now.Before(r.partySkillNextAt) {
				r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
			}
		} else {
			r.partySkillRecoverAt = now.Add(partySkillRecoveryRetry)
		}
		return
	}
	idle := now.Sub(r.partyDungeonLastAt)
	if idle > partyDungeonActivityTimeout {
		r.partySkillNextAt = time.Time{}
		if idle > partyDungeonActivityTimeout+partySkillRecoveryGrace {
			r.partySkillRecoverAt = time.Time{}
		}
		return
	}
	if now.Before(r.partySkillBlockedUntil) {
		return
	}
	if !r.partySkillRecoverAt.IsZero() || r.partySkillNextAt.IsZero() || now.Before(r.partySkillNextAt) {
		return
	}
	r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
	if !r.ensurePartySkillProfileUnsafe() || len(r.partySkillCandidates) == 0 {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 || r.partySelfPeer.uniqueID == 0 {
		return
	}
	if r.partyReliablePurposePendingUnsafe(peer.slot, "skill-") {
		r.partySkillNextAt = now.Add(time.Second)
		return
	}
	if r.partyReliablePendingCountUnsafe(peer.slot) > 0 {
		r.partySkillNextAt = now.Add(time.Second)
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
	if peer.uniqueID == 0 || r.partySelfPeer.uniqueID == 0 {
		return false
	}
	body := buildPartySkillStateBody(r.partySelfPeer.uniqueID, state, stateData, partySkillToken(r.UID, now))
	purpose := "skill-cast"
	if reason == "RECOVER" {
		purpose = "skill-recover"
	}
	route := r.partyRouteForPeerUnsafe(peer.slot)
	destination, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, [][]byte{body}, purpose, now)
	if err != nil {
		if r.shouldLogPartyRuntimeErrorUnsafe(now) {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s_ERROR] uid=%d state=%d data=%x route=%d destination=%s err=%v\n", reason, r.UID, state, stateData, route, destination, err)
		}
		return false
	}
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
	if !r.partyDungeonEnteredAt.IsZero() && now.Sub(r.partyDungeonEnteredAt) <= partyDungeonEntryTimeout && !r.partyDungeonLastAt.IsZero() && now.Sub(r.partyDungeonLastAt) <= partyDungeonActivityTimeout {
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
	matches, stats := shared.MatchPartySkillStates(job, whitelist, pvfStates)
	candidates := make([]partySkillCandidate, 0, len(matches))
	for _, entry := range matches {
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

func (r *RobotVo) partySkillDelayUnsafe(now time.Time) time.Duration {
	robotMembers := 1
	for _, peer := range r.partyPeers {
		if isPartyRobotAccount(peer.accID) && peer.accID != r.UID {
			robotMembers++
		}
	}
	base := 4 + 3*(robotMembers-1)
	return time.Duration(base+partySkillChoice(r.UID, now, 6)) * time.Second
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
