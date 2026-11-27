package repository

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"testing"

	"robot/internal/shared"
)

func TestMaxSkillSlotBlobMatchesNativeFormat(t *testing.T) {
	shared.SetSkillLearningDefinitions([]shared.SkillLearningDefinition{
		{Job: 8, SkillIndex: 1, Maximum: 50, GrowMaximum: []int{10}},
		{Job: 8, SkillIndex: 26, Maximum: 70, GrowMaximum: []int{50}},
	})
	t.Cleanup(func() { shared.SetSkillLearningDefinitions(nil) })
	blob, err := maxRobotSkillSlots(8, 0)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(blob) != 408 || len(raw) != 408 || !bytes.Equal(raw[:4], []byte{1, 10, 26, 50}) {
		t.Fatalf("native skill slots: %x / %x", blob[:4], raw)
	}
}
