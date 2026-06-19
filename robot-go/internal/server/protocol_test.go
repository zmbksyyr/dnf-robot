package server

import "testing"

func TestKMPRequiresFullPattern(t *testing.T) {
	if got := KMP([]byte("abcd"), []byte("xxxabcY")); got != -1 {
		t.Fatalf("expected no match for partial suffix, got %d", got)
	}
	if got := KMP([]byte("abcd"), []byte("xxabcdyy")); got != 2 {
		t.Fatalf("expected full match at 2, got %d", got)
	}
}

func TestFindPackTailPosShortBufferDoesNotPanic(t *testing.T) {
	if pos, tailLen := FindPackTailPos([]byte("short"), 0); pos != -1 || tailLen != 0 {
		t.Fatalf("expected no tail in short buffer, got pos=%d len=%d", pos, tailLen)
	}
}

func TestFindPackTailPosPartialTail(t *testing.T) {
	buf := []byte("abcLKJD")
	pos, tailLen := FindPackTailPos(buf, 0)
	if pos != 3 || tailLen != 4 {
		t.Fatalf("expected partial tail at 3 len 4, got pos=%d len=%d", pos, tailLen)
	}
}
