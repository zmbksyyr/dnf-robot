package marketapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"robot/internal/foundation/layout"
)

const auctionGuardFixture = `
var nativeSearch = new NativeFunction(ptr(0x084D75BC), 'int', ['pointer']);
Interceptor.replace(ptr(0x084D75BC), new NativeCallback(function () { return 0; }, 'int', []));
var original = true;
`

func TestUpsertAuctionSearchGuardIsIdempotent(t *testing.T) {
	original := []byte(auctionGuardFixture)
	installed, changed, err := upsertAuctionSearchGuard(original)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first install must change the file")
	}
	if bytes.Count(installed, []byte(auctionSearchGuardBegin)) != 1 ||
		bytes.Count(installed, []byte(auctionSearchGuardReplace+"(ptr(0x084D75BC)")) != 1 {
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
	legacy := []byte(auctionSearchGuardBegin + "\nlegacy guard\n" + auctionSearchGuardEnd + "\n" + auctionGuardFixture)
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
		"Interceptor.replace =",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("guard must not contain %q", forbidden)
		}
	}
	if !strings.Contains(source, "Interceptor.replace(target, replacement)") {
		t.Fatal("guard must install only its explicit replacement")
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
	next, changed, err := upsertAuctionSearchGuard([]byte(block + block + auctionGuardFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || bytes.Count(next, []byte(auctionSearchGuardBegin)) != 1 {
		t.Fatalf("duplicate guards were not collapsed:\n%s", next)
	}
}

func TestRewriteAuctionSearchReplacementAcceptsFormatting(t *testing.T) {
	source := []byte("Interceptor.replace (\n ptr ( '0x084D75BC' ), callback);\n")
	next, err := rewriteAuctionSearchReplacement(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(next, []byte(auctionSearchGuardReplace+" (\n ptr")) {
		t.Fatalf("target call was not rewritten:\n%s", next)
	}
}

func TestRewriteAuctionSearchReplacementRejectsMissingTarget(t *testing.T) {
	if _, err := rewriteAuctionSearchReplacement([]byte("var original = true;\n")); err == nil {
		t.Fatal("missing target must be rejected")
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

func TestInstallAuctionSearchGuardStoresRecoveryCopyUnderConfig(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	if err := layout.New(configDir).Ensure(); err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "df_game_r.js")
	original := []byte(auctionGuardFixture)
	if err := os.WriteFile(target, original, 0640); err != nil {
		t.Fatal(err)
	}

	app := &App{configDir: configDir}
	defer app.Shutdown()
	result, err := app.InstallAuctionSearchGuard(AuctionSearchGuardRequest{Path: target})
	if err != nil {
		t.Fatal(err)
	}
	wantBackup, err := layout.New(configDir).AuctionGuardBackup(target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup != wantBackup || !result.Changed || !result.Installed {
		t.Fatalf("result = %+v, want backup %s", result, wantBackup)
	}
	backup, err := os.ReadFile(wantBackup)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("backup does not contain the pre-patch script")
	}
	adjacent, err := filepath.Glob(target + ".bak*")
	if err != nil {
		t.Fatal(err)
	}
	if len(adjacent) != 0 {
		t.Fatalf("external backup artifacts remain beside target: %v", adjacent)
	}
}
