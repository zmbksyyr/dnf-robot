package dnf

import (
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"
)

func TestOnRecvDataCompactsBatchedPacketsOnce(t *testing.T) {
	vo := newBufferedTestRobot()
	packet := make([]byte, 7)
	binary.LittleEndian.PutUint16(packet[1:3], 500)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))

	batch := make([]byte, 0, 128*len(packet)+3)
	for i := 0; i < 128; i++ {
		batch = append(batch, packet...)
	}
	batch = append(batch, packet[:3]...)
	vo.onRecvData(batch)
	if vo.recvSize != 3 || string(vo.recvBuffer[:3]) != string(packet[:3]) {
		t.Fatalf("fragment after batch size=%d data=%x", vo.recvSize, vo.recvBuffer[:vo.recvSize])
	}

	vo.onRecvData(packet[3:])
	if vo.recvSize != 0 {
		t.Fatalf("completed fragment left %d bytes", vo.recvSize)
	}
}

func TestPartySkillProfileLoadIsSingleflightAndDoesNotHoldRobotLock(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	vo := &RobotVo{
		UID:                17000001,
		CID:                900001,
		State:              StateRun,
		partyDungeonLastAt: time.Now(),
		partySkillLoad: func(uid uint32, cid int) (partySkillProfile, error) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return partySkillProfile{
				cid:            cid,
				job:            6,
				whitelistCount: 1,
				pvfCount:       1,
				candidates:     []partySkillCandidate{{skillIndex: 3, state: 22}},
				stats:          partySkillCandidateStats{PVFMatched: 1},
			}, nil
		},
	}

	vo.mu.Lock()
	if vo.ensurePartySkillProfileUnsafe() {
		t.Fatal("asynchronous profile load completed synchronously")
	}
	if vo.ensurePartySkillProfileUnsafe() {
		t.Fatal("in-flight profile load reported ready")
	}
	vo.mu.Unlock()
	<-started

	lockAcquired := make(chan struct{})
	go func() {
		vo.mu.Lock()
		vo.mu.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("profile loader held the Robot mutex")
	}
	if calls.Load() != 1 {
		t.Fatalf("profile loader calls=%d", calls.Load())
	}

	close(release)
	waitForPartySkillProfile(t, vo)
	vo.mu.Lock()
	defer vo.mu.Unlock()
	if !vo.partySkillLoaded || vo.partySkillLoading || vo.partySkillJob != 6 || len(vo.partySkillCandidates) != 1 {
		t.Fatalf("loaded profile state loaded=%t loading=%t job=%d candidates=%d", vo.partySkillLoaded, vo.partySkillLoading, vo.partySkillJob, len(vo.partySkillCandidates))
	}
}

func TestPartySkillProfileResetRejectsStaleLoad(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	vo := &RobotVo{
		UID:   17000001,
		CID:   900001,
		State: StateRun,
		partySkillLoad: func(_ uint32, cid int) (partySkillProfile, error) {
			defer close(finished)
			started <- struct{}{}
			<-release
			return partySkillProfile{cid: cid, job: 6, candidates: []partySkillCandidate{{skillIndex: 3, state: 22}}}, nil
		},
	}

	vo.mu.Lock()
	vo.ensurePartySkillProfileUnsafe()
	vo.mu.Unlock()
	<-started
	vo.mu.Lock()
	vo.resetPartySkillProfileUnsafe()
	vo.mu.Unlock()
	close(release)
	<-finished
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		vo.mu.Lock()
		loaded := vo.partySkillLoaded
		loading := vo.partySkillLoading
		candidates := len(vo.partySkillCandidates)
		vo.mu.Unlock()
		if loaded || loading || candidates != 0 {
			t.Fatalf("stale profile survived reset: loaded=%t loading=%t candidates=%d", loaded, loading, candidates)
		}
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkOnRecvDataBatchedPackets(b *testing.B) {
	packet := make([]byte, 7)
	binary.LittleEndian.PutUint16(packet[1:3], 500)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	batch := make([]byte, 0, 64*len(packet))
	for i := 0; i < 64; i++ {
		batch = append(batch, packet...)
	}
	vo := newBufferedTestRobot()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vo.onRecvData(batch)
	}
}

func newBufferedTestRobot() *RobotVo {
	const size = 4096
	return &RobotVo{
		State:         StateRun,
		recvBuffer:    make([]byte, size),
		recvMaxSize:   size,
		minBufferSize: size,
	}
}

func waitForPartySkillProfile(t *testing.T, vo *RobotVo) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		vo.mu.Lock()
		loaded := vo.partySkillLoaded
		vo.mu.Unlock()
		if loaded {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("party skill profile did not finish loading")
}
