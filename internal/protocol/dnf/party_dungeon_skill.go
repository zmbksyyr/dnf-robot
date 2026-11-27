package dnf

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"net"
	"sort"
	"sync"
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
	skillIndex        byte
	state             byte
	level             int
	name              string
	stateData         []byte
	risk              int
	path              string
	cooldown          time.Duration
	cooldownByLevelMS []int
	baseMPByLevel     []int
	mpRules           shared.SkillMPRules
	itemCostFree      bool
	materials         shared.SkillMaterialDefinition
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
	if partyDungeonFrameUsesLegacySkills(frame, peer.uniqueID) {
		r.partyDungeonLegacySkills = true
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
	// Loading changes the room's actors and coordinate system. Neither queued
	// movement nor a reliable retry from the previous room may run in it.
	r.cancelPartyDungeonReliableUnsafe(now)
	r.partyDungeonFollow = nil
	previous := r.partyDungeonWorld
	from, known := previous.entryFrom, previous.entryFromKnown
	if actor, ok := previous.actors[partyDungeonPlayerReference(r.partySelfPeer.uniqueID)]; ok && actor.position.known&3 == 3 {
		from, known = actor.room, true
	}
	r.partyDungeonWorld = partyDungeonWorld{entryFrom: from, entryFromKnown: known}
	r.partyDungeonFlags = 0
	r.partyBasicAttackNextAt = time.Time{}
	r.partyBasicAttackStep = 0
	r.partyBasicAttackTarget = [4]byte{}
	r.partyBasicProjectiles = nil
	r.partySkillRecoverAt = time.Time{}
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout {
		r.resetPartySkillProfileUnsafe()
	}
	r.partyDungeonEnteredAt = now
	r.partyDungeonLoading = true
	r.partyDungeonLastAt = now
	r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
}

func (r *RobotVo) clearPartyDungeonRuntimeUnsafe() {
	r.partyDungeonIdentity = dungeonDefinitionIdentity{}
	if r.partyDungeonEnteredAt.IsZero() && !r.partyDungeonLoading && r.partyDungeonLastAt.IsZero() && r.partySkillNextAt.IsZero() && r.partyBasicAttackNextAt.IsZero() && r.partySkillRecoverAt.IsZero() && len(r.partyDungeonFollow) == 0 {
		return
	}
	r.cancelPartyDungeonReliableUnsafe(time.Now())
	r.partyDungeonFollow = nil
	r.partyDungeonEnteredAt = time.Time{}
	r.partyDungeonLoading = false
	r.partyDungeonRoomAt = time.Time{}
	r.partyDungeonLastAt = time.Time{}
	r.partyDungeonFlags = 0
	r.partyDungeonWorld = partyDungeonWorld{}
	r.partyBasicProjectiles = nil
	r.partyDungeonLegacySkills = false
	r.partySkillNextAt = time.Time{}
	r.partyBasicAttackNextAt = time.Time{}
	r.partyBasicAttackStep = 0
	r.partyBasicAttackTarget = [4]byte{}
	r.partySkillRecoverAt = time.Time{}
	r.partySkillBlockedUntil = time.Time{}
}

func (r *RobotVo) flushPartyDungeonSkillUnsafe(conn *net.UDPConn, now time.Time) {
	enabled := shared.PartySkillEnabled()
	if !enabled {
		r.cancelPartySkillCastsUnsafe(now)
		r.partyBasicProjectiles = nil
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		r.partyBasicAttackNextAt = time.Time{}
		r.partySkillNextAt = time.Time{}
	}
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout {
		r.clearPartyDungeonRuntimeUnsafe()
		return
	}
	if r.partyDungeonLoading {
		return
	}
	if r.partyDungeonLastAt.IsZero() {
		r.partySkillNextAt = time.Time{}
		r.partySkillRecoverAt = time.Time{}
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return
	}
	if !r.partySkillRecoverAt.IsZero() && !now.Before(r.partySkillRecoverAt) {
		// A late ACK must not resume an expired combo after recovery.
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
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
	if !enabled {
		return
	}
	idle := now.Sub(r.partyDungeonLastAt)
	if idle > partyDungeonActivityTimeout {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		r.partySkillNextAt = time.Time{}
		if idle > partyDungeonActivityTimeout+partySkillRecoveryGrace {
			r.partySkillRecoverAt = time.Time{}
		}
		return
	}
	if now.Before(r.partySkillBlockedUntil) {
		r.partyBasicAttackStep = 0
		r.partyBasicAttackTarget = [4]byte{}
		return
	}
	if !r.partySkillRecoverAt.IsZero() {
		if r.partyBasicAttackStep != 0 {
			r.tryPartyBasicAttackUnsafe(conn, now)
		}
		return
	}
	if r.partySkillNextAt.IsZero() || now.Before(r.partySkillNextAt) {
		r.tryPartyBasicAttackUnsafe(conn, now)
		return
	}
	for _, pending := range r.partyDungeonFollow {
		if !now.Before(pending.due) {
			r.partySkillNextAt = now.Add(time.Second)
			return
		}
	}
	r.partySkillNextAt = now.Add(r.partySkillDelayUnsafe(now))
	if !r.ensurePartySkillProfileUnsafe() {
		return
	}
	if len(r.partySkillCandidates) == 0 {
		r.tryPartyBasicAttackUnsafe(conn, now)
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
	// Follow traffic may occupy part of the reliable window continuously.
	// The transport enforces its window; only another skill must finish first.
	candidate, ready := r.nextPartySkillCandidateUnsafe(now)
	if !ready {
		r.tryPartyBasicAttackUnsafe(conn, now)
		return
	}
	if r.sendPartySkillStateUnsafe(conn, now, candidate.state, candidate.stateData, "CAST") {
		r.partySkillLastIndex = candidate.skillIndex
		if cooldown := candidate.cooldownAtLevel(); cooldown > 0 {
			if r.partySkillReadyAt == nil {
				r.partySkillReadyAt = make(map[byte]time.Time)
			}
			r.partySkillReadyAt[candidate.skillIndex] = now.Add(cooldown)
		}
		r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL] uid=%d job=%d skill=%d state=%d level=%d name=%s data=%x risk=%d path=%s recover_in=%s\n", r.UID, r.partySkillJob, candidate.skillIndex, candidate.state, candidate.level, candidate.name, candidate.stateData, candidate.risk, candidate.path, r.partySkillRecoverAt.Sub(now))
	}
}

func (c partySkillCandidate) cooldownAtLevel() time.Duration {
	if len(c.cooldownByLevelMS) == 0 {
		return c.cooldown
	}
	level := min(max(c.level, 1), len(c.cooldownByLevelMS))
	return time.Duration(c.cooldownByLevelMS[level-1]) * time.Millisecond
}

func (r *RobotVo) nextPartySkillCandidateUnsafe(now time.Time) (partySkillCandidate, bool) {
	if len(r.partySkillCandidates) == 0 {
		return partySkillCandidate{}, false
	}
	// Choose skills before their state aliases, so one skill with several
	// registered states cannot crowd out the rest of a character's repertoire.
	var seen [256]bool
	var ready []byte
	for _, candidate := range r.partySkillCandidates {
		if candidate.itemCostFree && !now.Before(r.partySkillReadyAt[candidate.skillIndex]) && !seen[candidate.skillIndex] {
			seen[candidate.skillIndex] = true
			ready = append(ready, candidate.skillIndex)
		}
	}
	if len(ready) == 0 {
		return partySkillCandidate{}, false
	}
	if len(ready) > 1 {
		for i, index := range ready {
			if index == r.partySkillLastIndex {
				ready = append(ready[:i], ready[i+1:]...)
				break
			}
		}
	}
	selected := ready[partySkillChoice(r.UID, now, len(ready))]
	var states []partySkillCandidate
	for _, candidate := range r.partySkillCandidates {
		if candidate.skillIndex == selected && candidate.itemCostFree {
			states = append(states, candidate)
		}
	}
	return states[partySkillChoice(r.UID^uint32(selected), now, len(states))], true
}

func (r *RobotVo) sendPartySkillStateUnsafe(conn *net.UDPConn, now time.Time, state byte, stateData []byte, reason string) bool {
	if err := shared.ValidatePartySkillStateData(r.partySkillJob, int(state), stateData); err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s_ERROR] uid=%d job=%d state=%d err=%v\n", reason, r.UID, r.partySkillJob, state, err)
		return false
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 || r.partySelfPeer.uniqueID == 0 {
		return false
	}
	body := buildPartySkillStateBody(r.partySelfPeer.uniqueID, state, stateData, partySkillToken(r.UID, now))
	if r.partyDungeonLegacySkills {
		body = buildPartyLegacySkillStateBody(r.partySelfPeer.uniqueID, state, stateData, partySkillToken(r.UID, now))
	}
	records := [][]byte{body}
	var targetID [4]byte
	if reason == "CAST" {
		// These are combat casts. Empty rooms should leave the actor free to
		// regroup; a target behind us must be faced before its skill state.
		self := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
		room, roomKnown := r.partyDungeonWorld.combatRoom(self, partyDungeonPlayerReference(peer.uniqueID))
		if !roomKnown {
			return false
		}
		position := r.partyDungeonWorld.actors[self].position
		selected, target, ok := r.partyDungeonWorld.nearestTarget(self, position)
		if !ok {
			return false
		}
		face, ok := buildPartyAttackFacing(r.partySelfPeer.uniqueID, position, target, room)
		if !ok {
			return false
		}
		records = [][]byte{face, body}
		targetID = selected
	}
	purpose := "skill-cast"
	if reason == "RECOVER" {
		purpose = "skill-recover"
	}
	route := r.partyRouteForPeerUnsafe(peer.slot)
	destination, err := r.sendPartyReliableUnsafe(conn, peer, r.partyDungeonFlags, records, purpose, now)
	if err != nil {
		if r.shouldLogPartyRuntimeErrorUnsafe(now) {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s_ERROR] uid=%d state=%d data=%x route=%d destination=%s err=%v\n", reason, r.UID, state, stateData, route, destination, err)
		}
		return false
	}
	if reason == "CAST" {
		r.partyDungeonWorld.noteTargetAttack(targetID, now)
	}
	r.recordPartyDungeonBodyUnsafe(peer.accID, "TX", body)
	return true
}

func (r *RobotVo) ensurePartySkillProfileUnsafe() bool {
	catalogGeneration := shared.PartySkillStateGeneration()
	if r.partySkillLoaded && r.partySkillCatalogGeneration != catalogGeneration {
		r.resetPartySkillProfileUnsafe()
	}
	if r.partySkillLoaded {
		return true
	}
	if !r.partySkillInfoKnown {
		// The learned-skill notification never parsed, so there is nothing to
		// match the catalog against. Say so instead of failing silently.
		if now := time.Now(); !now.Before(r.partySkillInfoDiagAt) {
			r.partySkillInfoDiagAt = now.Add(10 * time.Second)
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_SKIP] uid=%d cid=%d reason=skill-info-unknown\n", r.UID, r.CID)
		}
		return false
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
	if !startRobotRoutine(r.Controller, "party_skill_profile", uid, func() {
		r.loadPartySkillProfile(generation, catalogGeneration, uid, cid, loader)
	}) {
		r.partySkillLoading = false
	}
	return false
}

func (r *RobotVo) loadPartySkillProfile(generation, catalogGeneration uint64, uid uint32, cid int, loader partySkillProfileLoadFunc) {
	defer func() {
		if rec := recover(); rec != nil {
			r.mu.Lock()
			if generation == r.partySkillGeneration && r.UID == uid && r.State != StateStop {
				r.partySkillLoading = false
			}
			r.mu.Unlock()
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_ERROR] uid=%d cid=%d panic=%v\n", uid, cid, rec)
		}
	}()
	profile, err := loader(uid, cid)
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.partySkillGeneration || catalogGeneration != shared.PartySkillStateGeneration() || r.UID != uid || r.State == StateStop {
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
	r.partySkillCatalogGeneration = catalogGeneration
	r.partySkillJob = profile.job
	r.partySkillCandidates = profile.candidates[:0]
	for _, candidate := range profile.candidates {
		if learned := int(r.partySkillLevels[candidate.skillIndex]); learned > 0 {
			// SET_STATE carries no cast-level override. Resolve resource tables
			// from the character's learned level even when an older catalog
			// entry declares a lower level.
			candidate.level = learned
			r.partySkillCandidates = append(r.partySkillCandidates, candidate)
		}
	}
	if !r.partyDungeonEnteredAt.IsZero() && now.Sub(r.partyDungeonEnteredAt) <= partyDungeonEntryTimeout && !r.partyDungeonLastAt.IsZero() && now.Sub(r.partyDungeonLastAt) <= partyDungeonActivityTimeout {
		r.partySkillNextAt = now
	}
	regen, regenKnown := r.characterBaseInfo.baseMPRegen(profile.job)
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE] uid=%d cid=%d job=%d auto=%t whitelist=%d pvf=%d pvf_matched=%d kept_without_pvf=%d learned_candidates=%d skipped_missing_pvf=%d skipped_path_mismatch=%d skipped_unknown_state_data=%d base_mp_regen=%d base_mp_regen_known=%t\n", uid, profile.cid, profile.job, shared.PartySkillAutoSelection(), profile.whitelistCount, profile.pvfCount, profile.stats.PVFMatched, profile.stats.KeptWithoutPVF, len(r.partySkillCandidates), profile.stats.SkippedMissingPVF, profile.stats.SkippedPathMismatch, profile.stats.SkippedUnknownStateData, regen, regenKnown)
}

func (r *RobotVo) resetPartySkillProfileUnsafe() {
	r.partySkillLastIndex = 0
	r.partyBasicAttackStep = 0
	r.partyBasicAttackTarget = [4]byte{}
	r.partySkillGeneration++
	r.partySkillLoaded = false
	r.partySkillLoading = false
	r.partySkillJob = 0
	r.partySkillCandidates = nil
	r.partySkillCatalogGeneration = 0
}

// A character's job never changes, so the database row behind a skill profile is
// immutable. Batch scheduling logs the same account in and out repeatedly, and
// each login used to cost one query; remember the row for the process lifetime.
type partySkillIdentity struct {
	uid, cid int
}

var partySkillIdentityCache sync.Map // partySkillIdentity -> partySkillDBRow

type partySkillDBRow struct {
	cid, job int
}

func queryPartySkillProfile(db *sql.DB, uid uint32, cid int) (partySkillProfile, error) {
	identity := partySkillIdentity{uid: int(uid), cid: cid}
	if cached, ok := partySkillIdentityCache.Load(identity); ok {
		return partySkillProfileFromRow(cached.(partySkillDBRow)), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var row partySkillDBRow
	if err := db.QueryRowContext(ctx, "SELECT c.charac_no,c.job FROM taiwan_cain.charac_info c WHERE c.m_id=? AND (?=0 OR c.charac_no=?) AND c.delete_flag=0 ORDER BY c.charac_no LIMIT 1", uid, cid, cid).Scan(&row.cid, &row.job); err != nil {
		return partySkillProfile{}, err
	}
	partySkillIdentityCache.Store(identity, row)
	return partySkillProfileFromRow(row), nil
}

func partySkillProfileFromRow(row partySkillDBRow) partySkillProfile {
	profile := partySkillProfile{cid: row.cid, job: row.job}
	whitelist := shared.PartySkillStatesForJob(profile.job)
	pvfStates := shared.SkillStatesForJob(profile.job)
	profile.whitelistCount = len(whitelist)
	profile.pvfCount = len(pvfStates)
	profile.candidates, profile.stats = partySkillCandidatesFromCatalog(profile.job, whitelist, pvfStates)
	return profile
}

func partySkillCandidatesFromCatalog(job int, whitelist []shared.PartySkillState, pvfStates []shared.SkillState) ([]partySkillCandidate, partySkillCandidateStats) {
	return partySkillCandidatesFromCatalogMode(shared.PartySkillAutoSelection(), job, whitelist, pvfStates)
}

func partySkillCandidatesFromCatalogMode(auto bool, job int, whitelist []shared.PartySkillState, pvfStates []shared.SkillState) ([]partySkillCandidate, partySkillCandidateStats) {
	matches, stats := shared.MatchPartySkillStatesMode(auto, job, whitelist, pvfStates)
	candidates := make([]partySkillCandidate, 0, len(matches))
	for _, entry := range matches {
		candidates = append(candidates, partySkillCandidate{
			skillIndex:        byte(entry.SkillIndex),
			state:             byte(entry.State),
			level:             entry.Level,
			name:              entry.Name,
			stateData:         bytes.Clone(entry.StateData),
			risk:              entry.Risk,
			path:              entry.ScriptPath,
			cooldown:          time.Duration(entry.CooldownMS) * time.Millisecond,
			cooldownByLevelMS: append([]int(nil), entry.CooldownByLevelMS...),
			baseMPByLevel:     append([]int(nil), entry.BaseMPByLevel...),
			mpRules:           entry.MPRules,
			itemCostFree:      entry.ItemCostFree,
			materials:         entry.Materials.Clone(),
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
	return time.Duration(4+partySkillChoice(r.UID, now, 3)) * time.Second
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
