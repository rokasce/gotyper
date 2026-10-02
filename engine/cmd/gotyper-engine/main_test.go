package main

import (
	"strings"
	"testing"
)

func TestSelftestPasses(t *testing.T) {
	var out strings.Builder
	if err := selftest(&out); err != nil {
		t.Fatalf("selftest: %v", err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("summary = %q", out.String())
	}
	t.Log("\n" + out.String())
}

func TestBadFlagsExitNonZero(t *testing.T) {
	if code := run([]string{"--nope"}); code == 0 {
		t.Fatal("unknown flag should fail")
	}
	if code := run([]string{"--selftest", "extra"}); code == 0 {
		t.Fatal("extra arguments should fail")
	}
}
