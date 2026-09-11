package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNoticePriorityIsIndependentOfMessage(t *testing.T) {
	high := headerNotice{message: "Highlight an item", severity: noticeWarning, priority: noticeSelection}
	low := headerNotice{message: "Needs attention: arbitrary wording", severity: noticeError, priority: noticePrompt}
	for _, notices := range [][]headerNotice{{low, high}, {high, low}} {
		if got := preferredNotice(notices...); got != high {
			t.Fatal(got)
		}
	}
	if got := preferredNotice(headerNotice{priority: noticeSelection}, low); got != low {
		t.Fatal("empty notice displaced content", got)
	}
}

func TestResourceHeaderNoticePrecedence(t *testing.T) {
	m := listModel{browseAll: true, dimension: "project"}
	if got := m.resourceHeaderNotice(); got.priority != noticeScope || !strings.Contains(got.message, "All resources") {
		t.Fatal(got)
	}
	m.nextShellPreview = &LaunchPreview{}
	if got := m.resourceHeaderNotice(); got.priority != noticePrompt {
		t.Fatal(got)
	}
	m.pickerDescription = "Saved workspace"
	if got := m.resourceHeaderNotice(); got.priority != noticeStatus || got.severity != noticeSuccess {
		t.Fatal(got)
	}
	m.nextShellPreview.Error = "incompatible project"
	if got := m.resourceHeaderNotice(); got.priority != noticeSelection || got.severity != noticeWarning || !got.detailsAvailable {
		t.Fatal(got)
	}
	m.nextShellPreview.NeedsIdentity = true
	if got := m.resourceHeaderNotice(); !strings.Contains(got.message, "choose an identity") {
		t.Fatal(got)
	}
}

func TestExplicitNoticeSeverityOverridesWording(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		severity noticeSeverity
	}{{"success", noticeSuccess}, {"warning", noticeWarning}, {"error", noticeError}} {
		for _, message := range []string{"failed", "Saved successfully", "Arbitrary revised wording"} {
			m := listModel{pickerDescription: message, statusKind: tc.kind}
			if got := m.resourceStatus(); got.severity != tc.severity {
				t.Fatal(got)
			}
		}
	}
}

func TestNoticeTruncationRetainsFullDetails(t *testing.T) {
	full := strings.Repeat("Full diagnostic\n", 20)
	for _, details := range []bool{false, true} {
		n := headerNotice{message: full, severity: noticeError, detailsAvailable: details}
		for _, width := range []int{10, 23, 24, 40} {
			got := ansi.Strip(n.Render(width))
			if strings.Contains(got, "i Details") != (details && width >= 24) || ansi.StringWidth(got) > width || strings.Contains(got, "\n") {
				t.Fatal(width, details, got)
			}
			if n.message != full {
				t.Fatal("rendering lost full message")
			}
		}
	}
}
