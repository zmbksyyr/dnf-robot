package dnf

import (
	"bytes"
	"compress/zlib"
	"testing"
)

func TestInflatePartyInfoRejectsDecompressionBomb(t *testing.T) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(make([]byte, 1024*1024+1)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := inflatePartyInfo(compressed.Bytes()); err == nil {
		t.Fatal("oversized party payload unexpectedly inflated")
	}
}
