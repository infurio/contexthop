package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestPartialDiscoveryIsWarningAndTotalFailureIsError(t *testing.T) {
	for _, tc := range []struct{ notice, want string }{
		{"Discovery partially complete: 76 projects and 10 clusters · 45 GKE disabled · 1 access denied.", "Warning:"},
		{"Discovery partially complete: 2 projects · 1 failed scopes.", "Warning:"},
		{"Discovery finished with errors: 0 projects · 1 failed scopes.", "Error:"},
		{"GKE disabled · cluster scan unavailable.", "Warning:"},
		{"Access denied · cluster scan unavailable.", "Warning:"},
		{"Billing disabled · cluster scan unavailable.", "Warning:"},
	} {
		m := listModel{width: 160, pickerDescription: tc.notice}
		notice := m.resourceStatus()
		got := ansi.Strip(notice.message)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: %s", tc.notice, got)
		}
	}
}
