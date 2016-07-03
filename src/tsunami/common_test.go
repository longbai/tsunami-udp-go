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
