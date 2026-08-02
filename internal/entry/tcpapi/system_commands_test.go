package tcpapi

import "testing"

func TestCappedProfileBufferReportsFullWritesAndBoundsStorage(t *testing.T) {
	buf := cappedProfileBuffer{limit: 4}
	n, err := buf.Write([]byte("abcdef"))
	if err != nil || n != 6 {
		t.Fatalf("write got n=%d err=%v", n, err)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("stored profile got %q want %q", got, "abcd")
	}
	if !buf.truncated {
		t.Fatal("truncation was not reported")
	}
}
