package mailnotify

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeEventSource struct {
	letterID  uint64
	postalID  uint64
	characNos []uint32
	err       error
}

func (s fakeEventSource) currentCursor(context.Context) (uint64, uint64, error) {
	return s.letterID, s.postalID, s.err
}

func (s fakeEventSource) eventsAfter(context.Context, uint64, uint64, int) (uint64, uint64, []uint32, error) {
	return s.letterID, s.postalID, append([]uint32(nil), s.characNos...), s.err
}

type fakeSender struct {
	chars []uint32
	err   error
}

func (s *fakeSender) NotifyNewMail(characNo uint32) error {
	if s.err != nil {
		return s.err
	}
	s.chars = append(s.chars, characNo)
	return nil
}

func TestCurrentCursorEstablishesBaselineWithoutPendingMail(t *testing.T) {
	n := &Notifier{source: fakeEventSource{letterID: 12, postalID: 34}}
	state, err := n.currentCursor()
	if err != nil {
		t.Fatal(err)
	}
	if state.LetterID != 12 || state.PostalID != 34 || len(state.Pending) != 0 {
		t.Fatalf("baseline = %+v", state)
	}
}

func TestPollCoalescesLetterAndPostalForOneCharacter(t *testing.T) {
	sender := &fakeSender{}
	n := &Notifier{
		source:      fakeEventSource{letterID: 11, postalID: 21, characNos: []uint32{100, 100}},
		sender:      sender,
		statePath:   filepath.Join(t.TempDir(), stateFileName),
		settleDelay: 0,
	}
	state := cursorState{LetterID: 10, PostalID: 20, Pending: make(map[string]int64)}
	if err := n.poll(&state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(sender.chars) != 1 || sender.chars[0] != 100 {
		t.Fatalf("notifications = %v, want [100]", sender.chars)
	}
	if len(state.Pending) != 0 || state.LetterID != 11 || state.PostalID != 21 {
		t.Fatalf("state = %+v", state)
	}
}

func TestPollKeepsFailedNotificationPending(t *testing.T) {
	sender := &fakeSender{err: errors.New("monitor unavailable")}
	n := &Notifier{
		source:      fakeEventSource{letterID: 2, postalID: 3, characNos: []uint32{200}},
		sender:      sender,
		statePath:   filepath.Join(t.TempDir(), stateFileName),
		settleDelay: 0,
	}
	state := cursorState{LetterID: 1, PostalID: 2, Pending: make(map[string]int64)}
	if err := n.poll(&state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Pending["200"]; !ok {
		t.Fatalf("failed notification was not retained: %+v", state)
	}
}
