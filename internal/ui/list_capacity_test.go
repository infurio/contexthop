package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestEveryTabUsesAvailableHeightAndScrollsPastTen(t *testing.T) {
	for _, screen := range []Screen{ScreenWorkspace, ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		t.Run(string(screen), func(t *testing.T) {
			rows := []Option{}
			for i := 0; i < 60; i++ {
				name := fmt.Sprintf("row-%03d", i)
				rows = append(rows, Option{Name: name, Label: name, Detail: "Details", Identity: true, Project: true, Kubernetes: true, Docker: true, IdentityAccount: name, ProjectID: name, KubernetesContext: name, KubernetesCluster: name, DockerContext: name})
			}
			picker := Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: rows}
			m := NewAppModel(AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{screen: picker}})
			resize := func(height int) {
				next, _ := m.Update(tea.WindowSizeMsg{Width: 118, Height: height})
				m = next.(AppModel)
			}
			count := func() int {
				view := ansi.Strip(m.View().Content)
				n := 0
				for _, row := range rows {
					if strings.Contains(view, row.Name) {
						n++
					}
				}
				return n
			}
			resize(60)
			large := count()
			minimum := 40

			if large < minimum {
				t.Fatalf("tall window shows only %d rows; want at least %d", large, minimum)
			}
			resize(24)
			small := count()
			if small >= large || small == 0 {
				t.Fatal("viewport did not shrink", small, large)
			}
			m, _ = updateApp(m, specialKey(tea.KeyEnd))
			if !strings.Contains(m.View().Content, "row-059") {
				t.Fatal("last row unreachable")
			}
			resize(60)
			if !strings.Contains(m.View().Content, "row-059") || count() < minimum {
				t.Fatal("resize lost cursor or left excessive blank space")
			}
			m, _ = updateApp(m, specialKey(tea.KeyHome))
			if !strings.Contains(m.View().Content, "row-000") {
				t.Fatal("Home did not restore first row")
			}
			m, _ = updateApp(m, textKey("/"))
			for _, character := range "row-049" {
				m, _ = updateApp(m, textKey(string(character)))
			}
			if count() != 1 {
				t.Fatal("search did not narrow rows", count())
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			if count() != 1 {
				t.Fatal("leaving editing changed results")
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			if count() < minimum {
				t.Fatal("clearing search did not refill window")
			}
			for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
				if ansi.StringWidth(line) > 118 {
					t.Fatal("row overflow")
				}
			}
		})
	}
}
