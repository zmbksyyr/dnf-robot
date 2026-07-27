package marketapp

import (
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
