package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPreviewMessagesKeepTabsAndResourceRowsStationary(t *testing.T) {
	for _, width := range []int{35, 80, 140} {
		for _, height := range []int{12, 14, 16, 28} {
			m := listModel{width: width, height: height, resourceBrowser: true, composeSelection: true, dimension: "docker",
				options: []Option{{Name: "desktop", Docker: true, DockerContext: "desktop"}}}
			tabRow, itemRow := -1, -1
			for _, phase := range []string{"ready", "identity", "error", "empty", "ready"} {
				p := &LaunchPreview{Available: true, Fields: []PickerField{{Label: "id", Value: "user@example.com"}, {Label: "docker", Value: "desktop"}}}
				switch phase {
				case "identity":
					p.NeedsIdentity = true
				case "error":
					p.Error = strings.Repeat("long diagnostic\n", 30)
				case "empty":
					p = &LaunchPreview{}
				}
				m.nextShellPreview = p
				lines := strings.Split(strings.TrimSuffix(ansi.Strip(m.resourceBrowserView()), "\n"), "\n")
				currentTab, currentItem := -1, -1
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "┌") {
						currentTab = i
					}
					if strings.Contains(line, "> desktop") {
						currentItem = i
					}
					if ansi.StringWidth(line) > width {
						t.Fatal("message overflow", width, height, phase, line)
					}
				}
				if tabRow < 0 {
					tabRow, itemRow = currentTab, currentItem
				}
				if len(lines) != height || currentTab <= 0 || currentItem <= currentTab || currentTab != tabRow || currentItem != itemRow {
					t.Fatalf("%dx%d %s moved tabs or rows: %d/%d -> %d/%d", width, height, phase, tabRow, itemRow, currentTab, currentItem)
				}
				message := lines[currentTab-1]
				if phase == "ready" && strings.TrimSpace(message) != "" {
					t.Fatal("cleared message row is not blank", message)
				}
				if phase == "error" && (!strings.Contains(message, "…") || !strings.Contains(message, "i Details")) {
					t.Fatal("long error lacks continuation", message)
				}
			}
		}
	}
}

func TestFullPreviewErrorAvailableInResourceDetailsWithoutARow(t *testing.T) {
	message := strings.Repeat("complete diagnostic\n", 50)
	m := NewAppModel(AppOptions{ResourceBrowser: true, ComposeSelection: true,
		InitialDraft: Draft{ScreenIdentity: "user"},
		Pickers:      map[Screen]Picker{ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true}},
		LaunchPreview: func(Screen, Option, Draft) LaunchPreview {
			return LaunchPreview{Available: true, Error: message}
		}})
	m, _ = updateApp(m, textKey("i"))
	if m.CurrentScreen() != "resource-details" || !strings.Contains(m.current().picker.ReadOnlyText, message) || m.draft[ScreenIdentity] != "user" {
		t.Fatal("full diagnostic lost or selection changed", m.current().picker)
	}
}

func TestEveryTabUsesOneStatusLineAboveTabs(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, width := range []int{35, 80, 140} {
			m := listModel{width: width, height: 24, resourceBrowser: true, composeSelection: true, dimension: string(tab.screen), nextShellPreview: &LaunchPreview{Available: true}, options: []Option{{Name: "unique-row", Project: true, ProjectID: "unique-row", Identity: true, IdentityAccount: "unique-row", Kubernetes: true, KubernetesCluster: "unique-row", Docker: true, DockerContext: "unique-row"}}}
			baseline := -1
			for _, message := range []string{"", "Clusters haven't been discovered for this account.", "Discovery complete", strings.Repeat("Refresh failed: diagnostic ", 30), ""} {
				m.pickerDescription = message
				lines := strings.Split(ansi.Strip(m.resourceBrowserView()), "\n")
				tabRow := -1
				for i, line := range lines {
					if strings.Contains(line, "┌") {
						tabRow = i
					}
				}
				if baseline < 0 {
					baseline = tabRow
				}
				if tabRow < 1 || tabRow != baseline {
					t.Fatalf("%s status moved tabs", tab.screen)
				}
				if (strings.TrimSpace(lines[tabRow-1]) != "") != (message != "") {
					t.Fatalf("%s status missing above tabs: %q", tab.screen, lines[tabRow-1])
				}
				if strings.Contains(lines[tabRow+1], "Status:") || strings.Contains(lines[tabRow+1], "Error:") {
					t.Fatal("status inside table")
				}
				if message != "" && len(m.resourceHeader()) != len((&listModel{width: width, height: 24, nextShellPreview: &LaunchPreview{Available: true}, resourceBrowser: true}).resourceHeader()) {
					t.Fatal("message adds header rows")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatal("message overflow")
					}
				}
			}
			m.browseAll = true
			if tab.screen == ScreenProject || tab.screen == ScreenKubernetes {
				lines := m.resourceHeader()
				if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "All resources") || len(m.resourcePanelHeader()) != 1 {
					t.Fatal("scope notice is not in reserved row")
				}
			}
		}
	}
}

func TestStatusDetailsRemainAccessibleWithoutRows(t *testing.T) {
	message := strings.Repeat("Full discovery diagnostic. ", 30)
	m := NewAppModel(AppOptions{StartScreen: ScreenKubernetes, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{ScreenKubernetes: {Screen: ScreenKubernetes, Dimension: "kubernetes", ResourceBrowser: true, Description: message}}})
	m, _ = updateApp(m, textKey("i"))
	if m.CurrentScreen() != "resource-details" || !strings.Contains(m.current().picker.ReadOnlyText, strings.TrimSpace(message)) {
		t.Fatalf("full status unavailable without row: %s %q", m.CurrentScreen(), m.current().picker.ReadOnlyText)
	}
}
