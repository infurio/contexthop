package session

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/state"
)

type displayItem struct{ kind, label, value string }

// Only the activated selection's manifest supplies values, never ambient CLI
// configuration. Disabled components are implementation details, not selections.
func selectionItems(m state.Manifest) []displayItem {
	items := []displayItem{}
	add := func(kind, label, value string) {
		if value != "" {
			items = append(items, displayItem{kind, label, value})
		}
	}
	add("identity", "Identity", m.Expected.Identity)
	add("project", "Project", m.Expected.Project)
	if m.Expected.Kubernetes != "" {
		value := m.KubernetesLabel
		if value == "" {
			value = m.Expected.Kubernetes
		}
		add("kubernetes", "Kubernetes", value)
	}
	if m.Expected.Docker != disabledDockerContext {
		add("docker", "Docker", m.Expected.Docker)
	}
	return items
}

func validDisplayColor(value string) string {
	if config.ValidTagColor(value) {
		return value
	}
	return "39"
}

func displayColor(m state.Manifest, kind string) string {
	return validDisplayColor(m.PromptColors[kind])
}

func sortedDisplayTags(m state.Manifest) []string {
	names := make([]string, 0, len(m.DisplayTags))
	for name := range m.DisplayTags {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return '?'
		}
		return r
	}, value)
}

func summaryColor(text, color string, enabled bool) string {
	text = displayText(text)
	if !enabled {
		return text
	}
	if config.ValidTagColor(color) {
		rgb, _ := strconv.ParseUint(color[1:], 16, 32)
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", rgb>>16, (rgb>>8)&255, rgb&255, text)
	}
	// The only non-hex colors supplied here are our neutral/default palette.
	if color != "245" && color != "75" {
		color = "39"
	}
	return "\x1b[38;5;" + color + "m" + text + "\x1b[0m"
}

func formatSummary(m state.Manifest, namespace string, color bool) string {
	items := selectionItems(m)
	if len(items) == 0 {
		return ""
	}
	lines := []string{summaryColor("ContextHop", "245", color)}
	for _, item := range items {
		value := summaryColor(item.value, displayColor(m, item.kind), color)
		if item.kind == "kubernetes" && namespace != "" {
			value += summaryColor(" / ", "245", color) + summaryColor(namespace, "75", color)
		}
		lines = append(lines, summaryColor(item.label+": ", "245", color)+value)
	}
	if tags := displayTagsLine(m, color); tags != "" {
		lines = append(lines, tags)
	}
	return strings.Join(lines, "\n")
}

func displayTagsLine(m state.Manifest, color bool) string {
	tags := []string{}
	for _, name := range sortedDisplayTags(m) {
		tags = append(tags, summaryColor(name, validDisplayColor(m.DisplayTags[name]), color))
	}
	if len(tags) == 0 {
		return ""
	}
	return summaryColor("Tags: ", "245", color) + strings.Join(tags, summaryColor(" · ", "245", color))
}

// FormatStatus keeps the full observed status while sharing the summary palette.
func FormatStatus(snapshot state.Snapshot, m state.Manifest, color bool) string {
	contextName := snapshot.Destination
	if contextName == "" {
		if snapshot.Managed {
			contextName = "unnamed"
		} else {
			contextName = "unmanaged shell"
		}
	}
	lines := []string{summaryColor("Context: ", "245", color) + summaryColor(contextName, displayColor(m, "workspace"), color) + summaryColor(" ("+snapshot.LocalStatus+")", "245", color)}
	for _, item := range []displayItem{
		{"identity", "Identity", snapshot.Observed.Identity},
		{"project", "Cloud project", snapshot.Observed.Project},
		{"kubernetes", "Kubernetes", snapshot.Observed.Kubernetes},
		{"namespace", "Namespace", snapshot.Observed.Namespace},
		{"docker", "Docker", snapshot.Observed.Docker},
		{"adc", "ADC", snapshot.Observed.ADC},
	} {
		value, shade := item.value, displayColor(m, item.kind)
		if item.kind == "namespace" {
			shade = "75"
		}
		if value == "" || value == disabledDockerContext || strings.HasSuffix(value, "/disabled-google-credentials.json") {
			value, shade = "none", "245"
		}
		lines = append(lines, summaryColor(item.label+": ", "245", color)+summaryColor(value, shade, color))
	}
	if tags := displayTagsLine(m, color); tags != "" {
		lines = append(lines, tags)
	}
	return strings.Join(lines, "\n") + "\n"
}

func CurrentSummary() string {
	m, err := state.LoadManifest(os.Getenv(state.SessionFileEnv))
	if err != nil {
		return ""
	}
	namespace := m.Expected.Namespace
	if m.Expected.Kubernetes != "" {
		if _, current := state.KubernetesState(); current != "" {
			namespace = current
		}
	}
	return formatSummary(m, namespace, os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
}
