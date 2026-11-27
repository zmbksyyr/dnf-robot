package dnf

import (
	"bytes"
	"testing"
)

func TestMageProjectileMatchesOrdinaryClientCreate(t *testing.T) {
	// state-packet-cycle1: original state8/[0] at 11:58:16.924,
	// original 24202 creation at 11:58:17.096, stationary caster facing left.
	want := mustPartyHex(t, "000011019c01ffff000001008a5e00000000000000f0a9570000060400004d0100003b0000000c0000000000000000000000000000000000000100")
	body, ref := buildPartyMageProjectile(412, 0x57a9, 0,
		partyDungeonPosition{x: 1094.7755126953125, y: 332, known: 7}, false, 0xeca41d58)
	got, sender, ok := decodePartyPassiveObjectBody(body)
	if !ok || sender != 412 || ref != [4]byte{0, 0xf0, 0xa9, 0x57} || !bytes.Equal(got, want) {
		t.Fatalf("create=%x sender=%d ref=%x want=%x", got, sender, ref, want)
	}
}
