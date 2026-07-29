package marketapp

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpsertAuctionSearchGuardIsIdempotent(t *testing.T) {
	original := []byte("var original = true;\n")
	installed, changed, err := upsertAuctionSearchGuard(original)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first install must change the file")
	}
	if bytes.Count(installed, []byte(auctionSearchGuardBegin)) != 1 || !bytes.HasSuffix(installed, original) {
		t.Fatalf("unexpected installed source:\n%s", installed)
	}

	again, changed, err := upsertAuctionSearchGuard(installed)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(again, installed) {
		t.Fatal("second install must leave identical content")
	}
}

func TestUpsertAuctionSearchGuardUpgradesLegacyBlock(t *testing.T) {
	legacy := []byte(auctionSearchGuardBegin + "\nlegacy guard\n" + auctionSearchGuardEnd + "\n\nvar original = true;\n")
	next, changed, err := upsertAuctionSearchGuard(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("legacy guard must be upgraded")
	}
	if bytes.Count(next, []byte(auctionSearchGuardBegin)) != 1 || strings.Contains(string(next), "legacy guard") {
		t.Fatalf("legacy block was not replaced:\n%s", next)
	}
	if !strings.Contains(string(next), "compatible auction search installed") {
		t.Fatal("replacement guard source is missing")
	}
}

func TestAuctionSearchGuardOnlyOverlaysTrackedSocketData(t *testing.T) {
	source := auctionSearchGuardSource
	for _, forbidden := range []string{
		"Interceptor.attach",
		"Interceptor.revert",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("guard must not contain %q", forbidden)
		}
	}
	if !strings.Contains(source, "Interceptor.replace = rawReplace;") {
		t.Fatal("guard must restore Interceptor.replace after the blocked call")
	}
	if !strings.Contains(source, "rawReplace.call(Interceptor, target, replacement)") {
		t.Fatal("guard must forward unrelated replacements unchanged")
	}
	check := strings.Index(source, "socketData.add(0).readU8() === 0")
	copy := strings.Index(source, "Memory.copy(src.add(106 + 137 * i), socketData, 30)")
	if check < 0 || copy < 0 || check > copy {
		t.Fatal("guard must verify a DP2 socket record before overlaying native bytes")
	}
	if !strings.Contains(source, "return nativeSearch(dispatcher, user, src, a4)") {
		t.Fatal("guard must finish through the native auction search function")
	}
}

func TestUpsertAuctionSearchGuardCollapsesDuplicateBlocks(t *testing.T) {
	block := auctionSearchGuardBegin + "\nold\n" + auctionSearchGuardEnd + "\n\n"
	next, changed, err := upsertAuctionSearchGuard([]byte(block + block + "var original = true;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || bytes.Count(next, []byte(auctionSearchGuardBegin)) != 1 {
		t.Fatalf("duplicate guards were not collapsed:\n%s", next)
	}
}

func TestUpsertAuctionSearchGuardRejectsBrokenMarkers(t *testing.T) {
	for _, source := range []string{
		auctionSearchGuardBegin + "\nbroken",
		auctionSearchGuardEnd + "\nbroken",
	} {
		if _, _, err := upsertAuctionSearchGuard([]byte(source)); err == nil {
			t.Fatalf("expected marker error for %q", source)
		}
	}
}
