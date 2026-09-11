package main

import (
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func TestBrowserProfileAndDockerUnmapAreMenuOnly(t *testing.T) {
	cfg := config.New()
	cfg.Identities["account"] = config.Identity{Provider: "gcp", Account: "account@example.com"}
	cfg.Docker["local"] = config.Docker{Context: "local"}
	cfg.Destinations["work"] = config.Destination{Docker: "local"}
	for _, test := range []struct {
		screen               ui.Screen
		name, action, oldKey string
	}{
		{ui.ScreenIdentity, "account", "configure-browser", "b"},
		{ui.ScreenDocker, "local", "docker-unmap-workspace", "u"},
	} {
		picker := resourceBrowserPicker(cfg, test.screen, []ui.Option{{Name: test.name}}, "")
		found := false
		for _, action := range picker.Options[0].Actions {
			if action.Key == test.oldKey {
				t.Fatal("old shortcut remains", test.screen, action)
			}
			if action.Action == test.action {
				found = true
				if action.Key != "" || len(action.Aliases) != 0 {
					t.Fatal("menu-only action has a shortcut", action)
				}
			}
		}
		if !found {
			t.Fatal("menu-only action missing", test.screen, test.action)
		}
	}
}
