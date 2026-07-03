package random

import (
	"math/rand"
	"testing"
)

func TestBetweenAtLeastClampsMaxToMin(t *testing.T) {
	if got := BetweenAtLeast(rand.New(rand.NewSource(1)), 5, 3); got != 5 {
		t.Fatalf("clamped got %d want 5", got)
	}
}
