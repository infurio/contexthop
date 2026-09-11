package main

import (
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/ui"
)

func discoveryBlocked() (ui.Transition, bool) {
	if err := discovery.RequireUnmanagedShell(); err != nil {
		return ui.Transition{Picker: ui.Picker{Screen: "discovery-unavailable", Title: "Use an unmanaged shell", Description: err.Error(), CompactDialog: true, HideSearch: true, DisableEnter: true}}, true
	}
	return ui.Transition{}, false
}
