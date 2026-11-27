package dnf

import (
	"bytes"
	"testing"
)

func TestCharacterEquipmentCapturedModeOne(t *testing.T) {
	info, ok := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	if !ok || !info.equipment.known || info.equipment.count() != 2 || info.equipment.itemID(12) != 10400 || info.equipment.itemID(14) != 12400 {
		t.Fatalf("captured equipment = %+v, ok=%t", info.equipment, ok)
	}
	if info.equipment.itemID(10) != 0 || info.equipment.itemID(-1) != 0 || info.equipment.itemID(26) != 0 {
		t.Fatal("invented a weapon or an out-of-range item")
	}
	for _, slot := range []int{12, 14} {
		if upgrade, known := info.equipment.nativeUpgradeValue(slot); !known || upgrade != 0 {
			t.Fatalf("captured slot %d: native upgrade=%d known=%t", slot, upgrade, known)
		}
	}
	plain, err := inflatePartyInfo(mustPartyHex(t, capturedCharacterBaseInfo))
	if err != nil {
		t.Fatal(err)
	}
	gear, consumed, ok := parseCharacterEquipmentInfo(plain[96:])
	if !ok || consumed != 56 || gear != info.equipment || plain[96+consumed] != 0xff {
		t.Fatalf("equipment crossed into following USERINFO sections: consumed=%d ok=%t", consumed, ok)
	}
	for n := 0; n < consumed; n++ {
		partial := plain[:96+n]
		parsed, ok := parseCharacterBasicInfo(compressCharacterInfo(t, partial), 2)
		if !ok || parsed.baseMP() != 140 || parsed.equipment != (characterEquipmentInfo{}) {
			t.Fatalf("partial equipment at %d accepted or lost independent base stats: %+v", n, parsed)
		}
	}
	// Changed enchant data must invalidate the equipment snapshot even when
	// the item ID and base stats have not changed.
	changed := append([]byte(nil), plain...)
	changed[98+16] ^= 1
	updated, ok := parseCharacterBasicInfo(compressCharacterInfo(t, changed), 2)
	if !ok || updated == info || updated.equipment.itemID(12) != 10400 {
		t.Fatal("equipment option change was lost")
	}
}

func TestCharacterEquipmentNativeUpgradeSeparatesAttributeFlagsAndOtherFields(t *testing.T) {
	for _, test := range []struct {
		attribute byte
		want      int32
	}{
		{0, 0}, {0x20, 0}, {0xe0, 0}, {1, 1}, {0x21, 1}, {0xe1, 1}, {0xff, 31},
	} {
		// Ordinary armor record: no Chronicle or random options, followed by
		// separate upgrade and trade restriction. Distinct surrounding fields
		// prevent an offset error from appearing to be a valid upgrade value.
		record := mustPartyHex(t, "0c281200007856341200640044332211887766550299000000ff01")
		record[9] = test.attribute
		info, consumed, ok := parseCharacterEquipmentInfo(append([]byte{0, 1}, record...))
		if !ok || consumed != len(record)+2 {
			t.Fatal("invalid independent equipment record")
		}
		if upgrade, known := info.nativeUpgradeValue(12); !known || upgrade != test.want {
			t.Fatalf("attribute=%#x: upgrade=%d known=%t want=%d", test.attribute, upgrade, known, test.want)
		}
		for _, slot := range []int{-1, 11, 26} {
			if _, known := info.nativeUpgradeValue(slot); known {
				t.Fatalf("missing slot %d gained an upgrade value", slot)
			}
		}
	}
	for _, info := range []characterEquipmentInfo{
		{}, {known: true}, {known: true, slots: [26]string{12: "short"}},
		{known: true, slots: [26]string{12: "\x0c\x01\x00\x00\x00\x00\x00\x00\x00\x01"}},
		{slots: [26]string{12: "\x0c\x01\x00\x00\x00\x00\x00\x00\x00\x01"}},
	} {
		if _, known := info.nativeUpgradeValue(12); known {
			t.Fatal("incomplete or unknown equipment snapshot gained an upgrade value")
		}
	}
}

// Protocol fixtures derived from original MakeEquipList, Chronicle and random
// option serializers, rather than from the parser: an avatar with both binary
// blocks, two Chronicle options and four random options; and a creature with a
// remaining date. These are layout fixtures, not additional live captures.
const equipmentAvatarWithOptions = "0241420f000b000000142a003700000042000000024d001e000000000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d040000001122334402e604000000010309e70400000001030c03010203040506070809a902721020300901"
const equipmentCreature = "1648f600000b000000142a003700000042000000024d0040e2010000000001"

func TestCharacterEquipmentVariableRecords(t *testing.T) {
	avatar := mustPartyHex(t, equipmentAvatarWithOptions)
	creature := mustPartyHex(t, equipmentCreature)
	body := append([]byte{1, 2}, avatar...)
	body = append(body, creature...)
	full := append(append([]byte(nil), body...), 0xba, 0xdc, 0xfe)
	info, consumed, ok := parseCharacterEquipmentInfo(full)
	if !ok || !info.known || info.extraSlotStat != 1 || consumed != 133 || info.count() != 2 || info.itemID(2) != 1000001 || info.itemID(22) != 63048 {
		t.Fatalf("variable equipment: %+v consumed=%d ok=%t", info, consumed, ok)
	}
	if !bytes.Equal([]byte(info.slots[2]), avatar) || !bytes.Equal([]byte(info.slots[22]), creature) {
		t.Fatal("variable equipment options were not preserved exactly")
	}
	for n := 0; n < len(body); n++ {
		if info, _, ok := parseCharacterEquipmentInfo(body[:n]); ok || info != (characterEquipmentInfo{}) {
			t.Fatalf("accepted incomplete variable list at %d", n)
		}
	}
	// The fourth random option may be absent even when the first three exist.
	noFourth := append([]byte(nil), avatar[:94]...)
	noFourth[93] = 0xff
	noFourth = append(noFourth, avatar[98:]...)
	if got, n, ok := parseCharacterEquipmentInfo(append([]byte{0, 1}, noFourth...)); !ok || n != 98 || got.itemID(2) != 1000001 {
		t.Fatalf("absent fourth option: consumed=%d ok=%t", n, ok)
	}
	if empty, n, ok := parseCharacterEquipmentInfo([]byte{0, 0, 0xab}); !ok || !empty.known || empty.count() != 0 || n != 2 {
		t.Fatal("explicitly empty equipment list must be known")
	}
}

func TestCharacterEquipmentRejectsInvalidStructure(t *testing.T) {
	avatar := mustPartyHex(t, equipmentAvatarWithOptions)
	for _, change := range []struct {
		offset int
		value  byte
	}{
		{0, 26},  // slot out of range
		{23, 29}, // incorrect jewel block length
		{57, 5},  // incorrect avatar expansion length
		{65, 3},  // more than two Chronicle options
		{82, 4},  // more than three random options
		{93, 4},  // unsupported fourth option type
	} {
		bad := append([]byte(nil), avatar...)
		bad[change.offset] = change.value
		if _, _, ok := parseCharacterEquipmentInfo(append([]byte{0, 1}, bad...)); ok {
			t.Fatalf("accepted invalid equipment at %d", change.offset)
		}
	}
	for _, body := range [][]byte{
		{0, 27},
		append(append([]byte{0, 2}, avatar...), avatar...),
	} {
		if _, _, ok := parseCharacterEquipmentInfo(body); ok {
			t.Fatal("accepted excessive count or duplicate equipment slot")
		}
	}
}
