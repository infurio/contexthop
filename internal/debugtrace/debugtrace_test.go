package debugtrace

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFlushFormatsAndConsumesTrace(t *testing.T) {
	Enable()
	defer Disable()
	Record("Load configuration", 1250*time.Microsecond, "cache hit")

	var output bytes.Buffer
	Flush(&output)
	if value := output.String(); !strings.Contains(value, "1ms  Load configuration (cache hit)") {
		t.Fatalf("unexpected trace: %q", value)
	}
	output.Reset()
	Flush(&output)
	if output.Len() != 0 {
		t.Fatalf("second flush duplicated output: %q", output.String())
	}
}

func TestDisabledRecorderDoesNotWrite(t *testing.T) {
	Disable()
	Record("ignored", time.Second, "")
	var output bytes.Buffer
	Flush(&output)
	if output.Len() != 0 {
		t.Fatalf("disabled trace wrote %q", output.String())
	}
}
