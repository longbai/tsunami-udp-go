package tsunami

import (
	"testing"
	"time"
)

func TestMakeTranscriptFileName(t *testing.T) {
	ti := time.Unix(1234567890, 0)
	x := MakeTranscriptFileName(ti, "tsus")
	if x != "2009-02-14-07-31-30.tsus" {
		t.Fatal("test failed", x)
	}
}

func TestBZero(t *testing.T) {
	x := make([]byte, 2)
	x[1] = 1
	BZero(x)
	if x[1] != 0 || x[0] != 0 {
		t.Fatal("test failed", x)
	}
}
