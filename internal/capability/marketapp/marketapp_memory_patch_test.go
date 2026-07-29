package marketapp

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPatchPatternMatchAcceptsExpectedOrPatchedByteOnly(t *testing.T) {
	spec := auctionMemoryPatchSpec{
		name:         "test",
		expect:       0x07,
		value:        0x7f,
		targetOffset: 2,
		pattern:      []byte{0xaa, 0xbb, 0x00, 0xcc},
	}

	for _, b := range []byte{spec.expect, spec.value} {
		window := []byte{0xaa, 0xbb, b, 0xcc}
		if !patchPatternMatch(window, spec) {
			t.Fatalf("pattern should match target byte 0x%02x", b)
		}
	}
	if patchPatternMatch([]byte{0xaa, 0xbb, 0x46, 0xcc}, spec) {
		t.Fatal("pattern matched unexpected target byte")
	}
	if patchPatternMatch([]byte{0xaa, 0xbb, spec.expect, 0xcd}, spec) {
		t.Fatal("pattern matched changed surrounding bytes")
	}
}

func TestPatchPatternMatchAcceptsVersionAlternate(t *testing.T) {
	spec := auctionMemoryPatchSpec{
		name:         "level",
		expect:       0x46,
		alternates:   []byte{0x55},
		value:        0x7f,
		targetOffset: 2,
		pattern:      []byte{0xaa, 0xbb, 0x00, 0xcc},
	}

	for _, b := range []byte{0x46, 0x55, 0x7f} {
		if !patchPatternMatch([]byte{0xaa, 0xbb, b, 0xcc}, spec) {
			t.Fatalf("pattern should match supported byte 0x%02x", b)
		}
	}
	if patchPatternMatch([]byte{0xaa, 0xbb, 0x54, 0xcc}, spec) {
		t.Fatal("pattern matched unsupported version byte")
	}
}

func TestPatchPatternMatchAcceptsSafeCustomLevelRange(t *testing.T) {
	spec := auctionMemoryPatchSpec{
		name:         "level",
		expect:       0x46,
		alternates:   []byte{0x55},
		levelRange:   true,
		value:        0x7f,
		targetOffset: 2,
		pattern:      []byte{0xaa, 0xbb, 0x00, 0xcc},
	}

	for _, b := range []byte{1, 60, 70, 85, 90, 100, 126, 127} {
		if !patchPatternMatch([]byte{0xaa, 0xbb, b, 0xcc}, spec) {
			t.Fatalf("pattern should match safe level byte 0x%02x", b)
		}
	}
	for _, b := range []byte{0, 128, 255} {
		if patchPatternMatch([]byte{0xaa, 0xbb, b, 0xcc}, spec) {
			t.Fatalf("pattern matched unsafe level byte 0x%02x", b)
		}
	}
}

func TestLocateAuctionPatchAddressUsesUniqueExecutablePattern(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	spec := auctionMemoryPatchSpec{
		name:         "test",
		expect:       0x07,
		value:        0x7f,
		targetOffset: 2,
		pattern:      []byte{0xaa, 0xbb, 0x00, 0xcc},
	}
	data := []byte{0x00, 0xaa, 0xbb, spec.expect, 0xcc, 0x00}
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	addr, err := locateAuctionPatchAddress(file, []memorySegment{{start: 0, end: int64(len(data))}}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if addr != 3 {
		t.Fatalf("address = %d, want 3", addr)
	}
}

func TestLocateAuctionPatchAddressRejectsMultipleMatches(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	spec := auctionMemoryPatchSpec{
		name:         "test",
		expect:       0x07,
		value:        0x7f,
		targetOffset: 2,
		pattern:      []byte{0xaa, 0xbb, 0x00, 0xcc},
	}
	data := []byte{0xaa, 0xbb, spec.expect, 0xcc, 0x00, 0xaa, 0xbb, spec.value, 0xcc}
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	_, err = locateAuctionPatchAddress(file, []memorySegment{{start: 0, end: int64(len(data))}}, spec)
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("err = %v, want multiple match error", err)
	}
}

type failingPatchMemory struct {
	data   []byte
	writes int
	failAt int
}

func (m *failingPatchMemory) ReadAt(p []byte, off int64) (int, error) {
	return copy(p, m.data[int(off):]), nil
}

func (m *failingPatchMemory) WriteAt(p []byte, off int64) (int, error) {
	m.writes++
	if m.writes == m.failAt {
		return 0, errors.New("injected write failure")
	}
	return copy(m.data[int(off):], p), nil
}

func TestApplyAuctionMemoryPatchesRollsBackOnFailure(t *testing.T) {
	mem := &failingPatchMemory{data: []byte{70, 90}, failAt: 2}
	plans := []locatedAuctionMemoryPatch{
		{spec: auctionMemoryPatchSpec{name: "one", value: 127}, entryIndex: 0, address: 0, before: 70},
		{spec: auctionMemoryPatchSpec{name: "two", value: 127}, entryIndex: 1, address: 1, before: 90},
	}
	entries := []AuctionMemoryPatchEntry{{Name: "one", Before: 70, After: 70}, {Name: "two", Before: 90, After: 90}}

	patched, err := applyAuctionMemoryPatches(mem, plans, entries)
	if err == nil || patched != 0 {
		t.Fatalf("patched=%d err=%v", patched, err)
	}
	if mem.data[0] != 70 || mem.data[1] != 90 {
		t.Fatalf("memory was not rolled back: %v", mem.data)
	}
	if entries[0].Changed || entries[1].Changed {
		t.Fatalf("entries still marked changed: %+v", entries)
	}
}
