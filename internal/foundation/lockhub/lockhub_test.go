package lockhub

import (
	"testing"
	"time"
)

func TestWithResourceSerializesSameResource(t *testing.T) {
	h := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.WithResource("store", "slot-1", "test", func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- h.WithResource("store", "slot-1", "test", func() error { return nil })
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second resource lock entered early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}
