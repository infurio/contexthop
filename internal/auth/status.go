package auth

import (
	"context"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"sync"
	"time"
)

type authObservation struct {
	label string
	at    time.Time
}

var observations = struct {
	sync.Mutex
	values map[string]authObservation
}{values: map[string]authObservation{}}

func identityStatusKey(identity config.Identity) string {
	return identity.Provider + "\x00" + identity.Account + "\x00" + identity.CloudSDKConfig
}

// Status reports evidence from a token check, never merely an account listing.
func Status(identity config.Identity) string {
	observations.Lock()
	defer observations.Unlock()
	value, ok := observations.values[identityStatusKey(identity)]
	if !ok {
		return "Not checked"
	}
	if time.Since(value.at) > 2*time.Minute {
		return "Check expired"
	}
	return value.label
}
func RecordStatus(identity config.Identity, label string) {
	observations.Lock()
	defer observations.Unlock()
	observations.values[identityStatusKey(identity)] = authObservation{label: label, at: time.Now()}
}

// RefreshStatuses runs bounded checks off the UI thread, with limited concurrency.
func RefreshStatuses(cfg config.Config) {
	slots := make(chan struct{}, 3)
	var workers sync.WaitGroup
	for name, identity := range cfg.Identities {
		if identity.Hidden {
			continue
		}
		workers.Add(1)
		go func(name string, identity config.Identity) {
			defer workers.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = Check(ctx, resolver.Resolved{IdentityName: name, Identity: &identity})
		}(name, identity)
	}
	workers.Wait()
}

// An older background check must not overwrite a newer login or discovery result.
func recordStatusSince(identity config.Identity, label string, started time.Time) {
	observations.Lock()
	defer observations.Unlock()
	key := identityStatusKey(identity)
	if observations.values[key].at.After(started) {
		return
	}
	observations.values[key] = authObservation{label: label, at: time.Now()}
}
