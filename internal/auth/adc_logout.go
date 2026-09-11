package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

type revocableADC struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}

// LogoutADC revokes the configured user ADC, including custom paths. A temporary
// gcloud directory prevents the provider command from selecting ambient ADC or
// modifying a different default file. CLI credentials may share the revoked token.
func LogoutADC(ctx context.Context, name string, identity config.Identity) error {
	if identity.Provider != "gcp" {
		return fmt.Errorf("authentication provider %q is not implemented", identity.Provider)
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return err
	}
	if configDir == "" {
		return fmt.Errorf("identity %q has no isolated cloudSdkConfig", name)
	}
	selected, err := adcPath(identity, configDir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(selected)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read selected ADC: %w", err)
	}
	var credential revocableADC
	if json.Unmarshal(data, &credential) != nil || credential.Type != "authorized_user" || credential.RefreshToken == "" || credential.ClientID == "" {
		return fmt.Errorf("ADC logout requires user credentials created by Google login; selected ADC was left unchanged")
	}
	copies := map[string][]byte{selected: data}
	generated := filepath.Join(configDir, "application_default_credentials.json")
	if generated != selected {
		original, err := os.ReadFile(generated)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect default ADC copy: %w", err)
		}
		var other revocableADC
		if err == nil && json.Unmarshal(original, &other) == nil && other == credential {
			copies[generated] = original
		}
	}
	scratch, err := os.MkdirTemp("", "chop-adc-revoke-")
	if err != nil {
		return fmt.Errorf("prepare ADC logout: %w", err)
	}
	defer os.RemoveAll(scratch)
	staged := filepath.Join(scratch, "application_default_credentials.json")
	if err := os.WriteFile(staged, data, 0600); err != nil {
		return fmt.Errorf("stage ADC logout: %w", err)
	}
	command := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "revoke", "--quiet")
	command.Env = isolatedGoogleEnvironment(os.Environ(), map[string]string{"CLOUDSDK_CONFIG": scratch, "CLOUDSDK_CORE_ACCOUNT": identity.Account})
	// Provider diagnostics may contain credential data; do not echo them.
	if err := command.Run(); err != nil {
		return fmt.Errorf("revoke ADC for %s: %w; local ADC files were retained", identity.Account, err)
	}
	RecordStatus(identity, "Not checked")
	for path, original := range copies {
		current, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("ADC revoked, but cannot inspect local copy for removal: %w", err)
		}
		if !bytes.Equal(current, original) {
			return fmt.Errorf("ADC revoked, but a local credential file changed during logout and was retained; retry if needed")
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("ADC revoked, but cannot remove local credentials: %w", err)
		}
	}
	return nil
}
