package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type noticeSeverity uint8

const (
	noticeNeutral noticeSeverity = iota
	noticeWarning
	noticeError
	noticeSuccess
)

type noticePriority uint8

const (
	noticeScope noticePriority = iota
	noticePrompt
	noticeStatus
	noticeSelection
)

// Metadata controls precedence and presentation; message wording is content.
// Keep the full message for Details even when the header shows one short line.
type headerNotice struct {
	message          string
	severity         noticeSeverity
	priority         noticePriority
	detailsAvailable bool
}

func (n headerNotice) style() lipgloss.Style {
	switch n.severity {
	case noticeWarning:
		return warnStyle
	case noticeError:
		return dangerStyle
	case noticeSuccess:
		return headingStyle.Foreground(stagedStyle.GetForeground())
	default:
		return dimStyle
	}
}

func (n headerNotice) Render(width int) string {
	message := singleLine(n.message)
	if message == "" {
		return ""
	}
	if lipgloss.Width(message) > width && width >= 24 && n.detailsAvailable {
		const suffix = " · i Details"
		message = fitColumn(message, width-lipgloss.Width(suffix)) + suffix
	}
	return n.style().Render(fitColumn(message, width))
}

func preferredNotice(notices ...headerNotice) headerNotice {
	var chosen headerNotice
	for _, notice := range notices {
		if notice.message != "" && (chosen.message == "" || notice.priority > chosen.priority) {
			chosen = notice
		}
	}
	return chosen
}

func selectionNotice(preview *LaunchPreview) headerNotice {
	if preview == nil {
		return headerNotice{}
	}
	if preview.NeedsIdentity {
		return headerNotice{message: "Selected: choose an identity to continue", severity: noticeWarning, priority: noticeSelection, detailsAvailable: true}
	}
	if preview.Error != "" {
		return headerNotice{message: "Needs attention: " + preview.Error, severity: noticeWarning, priority: noticeSelection, detailsAvailable: true}
	}
	if !preview.Available && len(preview.Fields) == 0 {
		return headerNotice{message: "Highlight an item", priority: noticePrompt, detailsAvailable: true}
	}
	return headerNotice{}
}

func (m listModel) resourceStatus() headerNotice {
	message := strings.TrimSpace(ansi.Strip(m.pickerDescription))
	if message == "" {
		return headerNotice{}
	}
	var severity noticeSeverity
	switch m.statusKind {
	case "warning":
		severity = noticeWarning
	case "error":
		severity = noticeError
	case "success":
		severity = noticeSuccess
	default:
		severity = legacyStatusSeverity(message)
	}
	label := "Status"
	switch severity {
	case noticeWarning:
		label = "Warning"
	case noticeError:
		label = "Error"
	case noticeSuccess:
		label = "Success"
	}
	return headerNotice{message: label + ": " + message, severity: severity, priority: noticeStatus, detailsAvailable: true}
}

// Older picker descriptions lack StatusKind. Preserve their classification at
// this boundary; explicit kinds always win, and rendering never inspects words.
func legacyStatusSeverity(message string) noticeSeverity {
	lower := strings.ToLower(message)
	switch {
	case strings.HasPrefix(lower, "discovery partially complete"), strings.HasPrefix(lower, "gke disabled"), strings.HasPrefix(lower, "access denied"), strings.HasPrefix(lower, "billing disabled"):
		return noticeWarning
	case strings.Contains(lower, "failed"), strings.Contains(lower, "error"), strings.Contains(lower, "cannot "):
		return noticeError
	case strings.Contains(lower, "warning"), strings.Contains(lower, "cached"), strings.Contains(lower, "needs attention"):
		return noticeWarning
	case strings.Contains(lower, "saved"), strings.Contains(lower, "complete"), strings.Contains(lower, "undone"), strings.Contains(lower, "fetched"):
		return noticeSuccess
	default:
		return noticeNeutral
	}
}

func (m listModel) resourceHeaderNotice() headerNotice {
	var scope headerNotice
	if m.browseAll && (m.dimension == "project" || m.dimension == "kubernetes") {
		scope = headerNotice{message: "All resources · b to filter by selection", priority: noticeScope, detailsAvailable: true}
	}
	return preferredNotice(m.resourceStatus(), selectionNotice(m.nextShellPreview), scope)
}
