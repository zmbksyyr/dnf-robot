package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBoundedLogSinkRotatesWithoutDroppingInput(t *testing.T) {
	oldPath := *boundedLogSinkPath
	oldMaxBytes := *boundedLogMaxBytes
	oldBackups := *boundedLogMaxBackups
	defer func() {
		*boundedLogSinkPath = oldPath
		*boundedLogMaxBytes = oldMaxBytes
		*boundedLogMaxBackups = oldBackups
	}()

	path := filepath.Join(t.TempDir(), "stdout.log")
	*boundedLogSinkPath = path
	*boundedLogMaxBytes = 8
	*boundedLogMaxBackups = 2
	input := "0123456789abcdefghij"
	if err := runBoundedLogSink(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	for _, candidate := range []string{path + ".2", path + ".1", path} {
		data, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatalf("read %s: %v", candidate, err)
		}
		if len(data) > int(*boundedLogMaxBytes) {
			t.Fatalf("%s size = %d, limit = %d", candidate, len(data), *boundedLogMaxBytes)
		}
		output.Write(data)
	}
	if output.String() != input {
		t.Fatalf("rotated output = %q, want %q", output.String(), input)
	}
}
