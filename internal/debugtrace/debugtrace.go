// Package debugtrace provides opt-in, process-local timing diagnostics.
// Events are buffered so interactive terminal UIs are not corrupted by output.
package debugtrace

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Name     string
	Detail   string
	Duration time.Duration
}

var recorder struct {
	sync.Mutex
	enabled bool
	events  []Event
}

// Enable starts a fresh trace.
func Enable() {
	recorder.Lock()
	defer recorder.Unlock()
	recorder.enabled = true
	recorder.events = nil
}

// Disable discards the current trace.
func Disable() {
	recorder.Lock()
	defer recorder.Unlock()
	recorder.enabled = false
	recorder.events = nil
}

func Enabled() bool {
	recorder.Lock()
	defer recorder.Unlock()
	return recorder.enabled
}

// Record appends an event when tracing is enabled.
func Record(name string, duration time.Duration, detail string) {
	recorder.Lock()
	defer recorder.Unlock()
	if !recorder.enabled {
		return
	}
	recorder.events = append(recorder.events, Event{Name: name, Detail: detail, Duration: duration})
}

// Flush writes and removes all buffered events. Tracing remains enabled so a
// deferred flush can report errors without duplicating earlier output.
func Flush(writer io.Writer) {
	recorder.Lock()
	defer recorder.Unlock()
	if !recorder.enabled || len(recorder.events) == 0 {
		return
	}
	fmt.Fprintln(writer, "ContextHop debug trace:")
	for _, event := range recorder.events {
		name := event.Name
		if detail := strings.TrimSpace(event.Detail); detail != "" {
			name += " (" + detail + ")"
		}
		fmt.Fprintf(writer, "  %9s  %s\n", formatDuration(event.Duration), name)
	}
	recorder.events = nil
}

func formatDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Round(time.Millisecond)/time.Millisecond)
	}
	return duration.Round(10 * time.Millisecond).String()
}
