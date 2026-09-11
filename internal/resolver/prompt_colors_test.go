package resolver

import (
	"github.com/infurio/contexthop/internal/config"
	"testing"
)

func TestPromptColorsUseFirstDisplayedTag(t *testing.T) {
	cfg := config.Config{
		Tags:         map[string]config.Tag{"alpha": {Color: "#123456"}, "zulu": {Color: "#abcdef"}},
		Docker:       map[string]config.Docker{"engine": {Context: "engine", LabelSet: config.LabelSet{Tags: []string{"zulu", "alpha"}}}},
		Destinations: map[string]config.Destination{"work": {Docker: "engine", LabelSet: config.LabelSet{Tags: []string{"zulu"}}}},
	}
	got, err := Destination(cfg, "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.PromptColors["workspace"] != "#abcdef" || got.PromptColors["docker"] != "#123456" {
		t.Fatal(got.PromptColors)
	}
	if color := firstTagColor(cfg, config.LabelSet{}); color != "" {
		t.Fatal(color)
	}
	if color := firstTagColor(cfg, config.LabelSet{Tags: []string{"missing"}}); color != config.DefaultTagColor {
		t.Fatal(color)
	}
}
