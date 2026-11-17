//go:build windows

package marketapp

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"robot/internal/foundation/hostprofile"
)

func TestWindowsHostTargetsAndBankedRefinePatch(t *testing.T) {
	previous := hostprofile.Current()
	t.Cleanup(func() { hostprofile.Configure(previous) })
	profile := hostprofile.Default(nil)
	profile.MemoryModel = "refine-banks-31"
	for _, name := range []string{"game", "auction", "point"} {
		profile.Services[name] = hostprofile.Service{Executable: "host.exe", PIDFile: name + ".pid", Image: filepath.Join("C:\\instance", name, "df_"+name+"_r"),
			ImageSHA256: strings.Repeat("0", 64), Start: []string{"host.exe"}, Directory: `C:\instance`, StopEvent: `Local\stop-{pid}`}
	}
	hostprofile.Configure(profile)
	targets := DefaultConfig().ItemInfoTargets
	if len(targets) != 2 || targets[0] != `C:\instance\auction\iteminfo.dat` || targets[1] != `C:\instance\point\iteminfo.dat` {
		t.Fatalf("incorrect host index paths: %v", targets)
	}
	for _, spec := range platformAuctionPatchSpecs() {
		want := byte(127)
		if spec.name == "refine_average_price_valid" {
			want = 7
		}
		if spec.name == "refine_search_valid" {
			want = 31
		}
		if spec.value != want {
			t.Fatalf("%s value=%d want=%d", spec.name, spec.value, want)
		}
	}
	if auctionMemoryPatchSpecs[0].value != 127 {
		t.Fatal("Windows profile changed Linux patch values")
	}
}

func TestBankedHostPatchVerifiesPinnedAddressAndRejectsChanges(t *testing.T) {
	previous := hostprofile.Current()
	t.Cleanup(func() { hostprofile.Configure(previous) })
	p := hostprofile.Default(nil)
	p.MemoryModel = "refine-banks-31"
	s := p.Services["auction"]
	s.ImageSHA256 = "b31eafd5068b450929133365f071fc2d0dedc7a8a5b80f9c81da44ce4008290c"
	p.Services["auction"] = s
	hostprofile.Configure(p)
	spec := auctionMemoryPatchSpec{name: "refine", fallbackAddr: 2, expect: 7, value: 31,
		targetOffset: 2, pattern: []byte{0xaa, 0xbb, 0, 0xcc}}
	data := []byte{0xaa, 0xbb, 31, 0xcc, 0xaa, 0xbb, 7, 0xcc}
	addr, err := locateAuctionPatchAddress(bytes.NewReader(data), nil, spec)
	if err != nil || addr != 2 {
		t.Fatalf("pinned location: addr=%d err=%v", addr, err)
	}
	data[0] = 0
	if _, err := locateAuctionPatchAddress(bytes.NewReader(data), nil, spec); err == nil {
		t.Fatal("changed surrounding instructions accepted")
	}
	data[0] = 0xaa
	s.ImageSHA256 = strings.Repeat("0", 64)
	p.Services["auction"] = s
	hostprofile.Configure(p)
	if _, err := locateAuctionPatchAddress(bytes.NewReader(data), nil, spec); err == nil {
		t.Fatal("different ELF hash accepted")
	}
}
