package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

func Check(ctx context.Context, resolved resolver.Resolved) (result error) {
	started := time.Now()
	label := "Check failed"
	if resolved.Identity != nil {
		defer func() {
			if result == nil {
				label = "Authenticated"
			}
			recordStatusSince(*resolved.Identity, label, started)
		}()
	}

	if resolved.Identity == nil {
		return nil
	}
	if resolved.Identity.Provider != "gcp" {
		return fmt.Errorf("authentication provider %q is not implemented", resolved.Identity.Provider)
	}
	configDir, err := resolver.ExpandPath(resolved.Identity.CloudSDKConfig)
	if err != nil {
		return err
	}
	if configDir == "" {
		return fmt.Errorf("identity %q has no isolated cloudSdkConfig", resolved.IdentityName)
	}
	values := map[string]string{
		"CLOUDSDK_CONFIG":       configDir,
		"CLOUDSDK_CORE_ACCOUNT": resolved.Identity.Account,
	}
	if resolved.Project != nil {
		values["CLOUDSDK_CORE_PROJECT"] = resolved.Project.ProjectID
	}
	environment := isolatedGoogleEnvironment(os.Environ(), values)
	command := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token", "--account", resolved.Identity.Account, "--quiet")
	command.Env = environment
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		message := strings.ToLower(diagnostic.String())
		for _, marker := range []string{"reauth", "gcloud auth login", "invalid_grant", "no credential", "authentication required"} {
			if strings.Contains(message, marker) {
				label = "Sign-in required"
				break
			}
		}
		return fmt.Errorf("Google CLI authentication is required for %s", resolved.Identity.Account)
	}
	return nil
}

func CheckADC(ctx context.Context, resolved resolver.Resolved) error {
	if resolved.ADCMode == "" {
		return nil
	}
	if resolved.ADCMode != "identity" {
		return fmt.Errorf("unsupported ADC mode %q", resolved.ADCMode)
	}
	if resolved.Identity == nil {
		return fmt.Errorf("application-default authentication requires an identity")
	}
	configDir, err := resolver.ExpandPath(resolved.Identity.CloudSDKConfig)
	if err != nil {
		return err
	}
	if configDir == "" {
		return fmt.Errorf("identity %q has no isolated cloudSdkConfig", resolved.IdentityName)
	}
	adc, err := adcPath(*resolved.Identity, configDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(adc); err != nil {
		return fmt.Errorf("application-default authentication is required for %s", resolved.Identity.Account)
	}
	values := map[string]string{
		"CLOUDSDK_CONFIG":                configDir,
		"CLOUDSDK_CORE_ACCOUNT":          resolved.Identity.Account,
		"GOOGLE_APPLICATION_CREDENTIALS": adc,
	}
	if resolved.Project != nil {
		values["CLOUDSDK_CORE_PROJECT"] = resolved.Project.ProjectID
	}
	adcCommand := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token", "--quiet")
	adcCommand.Env = isolatedGoogleEnvironment(os.Environ(), values)
	token, err := adcCommand.Output()
	if err != nil {
		return fmt.Errorf("application-default authentication is expired for %s", resolved.Identity.Account)
	}
	return verifyADCIdentity(ctx, strings.TrimSpace(string(token)), resolved.Identity.Account)
}

func Login(ctx context.Context, name string, identity config.Identity) error {
	command, chromeProfile, err := LoginCommand(ctx, name, identity)
	if err != nil {
		return err
	}
	if chromeProfile != "" {
		fmt.Printf("Opening Google authentication in Chrome profile %s.\n", chromeProfile)
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("authenticate %s: %w", identity.Account, err)
	}
	return nil
}

// LoginCommand prepares an interactive provider login without starting it.
// TUI callers can hand it to Bubble Tea while preserving their navigation.
func LoginCommand(ctx context.Context, name string, identity config.Identity) (*exec.Cmd, string, error) {
	return loginCommand(ctx, name, identity, false)
}

// BrowserLoginCommand always starts fresh browser authorization.
func BrowserLoginCommand(ctx context.Context, name string, identity config.Identity) (*exec.Cmd, string, error) {
	return loginCommand(ctx, name, identity, true)
}

func loginCommand(ctx context.Context, name string, identity config.Identity, browser bool) (*exec.Cmd, string, error) {
	if identity.Provider != "gcp" {
		return nil, "", fmt.Errorf("authentication provider %q is not implemented", identity.Provider)
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return nil, "", err
	}
	if configDir == "" {
		return nil, "", fmt.Errorf("identity %q has no isolated cloudSdkConfig", name)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create isolated gcloud directory: %w", err)
	}
	environment := isolatedGoogleEnvironment(os.Environ(), map[string]string{
		"CLOUDSDK_CONFIG":       configDir,
		"CLOUDSDK_CORE_ACCOUNT": identity.Account,
	})
	environment, chromeProfile, err := configureChromeBrowser(environment, identity.Account, configDir)
	if err != nil {
		return nil, "", err
	}
	args := []string{"auth", "login", identity.Account}
	if browser {
		args = append(args, "--force", "--launch-browser")
	}
	command := exec.CommandContext(ctx, "gcloud", args...)
	command.Env = environment
	return command, chromeProfile, nil
}

func LoginADC(ctx context.Context, name string, identity config.Identity) error {
	command, chromeProfile, err := LoginADCCommand(ctx, name, identity)
	if err != nil {
		return err
	}
	if chromeProfile != "" {
		fmt.Printf("Opening Google authentication in Chrome profile %s.\n", chromeProfile)
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("authenticate ADC for %s: %w", identity.Account, err)
	}
	return CompleteADCLogin(identity)
}

// LoginADCCommand prepares the separate ADC login for an interactive TUI process.
func LoginADCCommand(ctx context.Context, name string, identity config.Identity) (*exec.Cmd, string, error) {
	if identity.Provider != "gcp" {
		return nil, "", fmt.Errorf("authentication provider %q is not implemented", identity.Provider)
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return nil, "", err
	}
	if configDir == "" {
		return nil, "", fmt.Errorf("identity %q has no isolated cloudSdkConfig", name)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create isolated gcloud directory: %w", err)
	}
	environment := isolatedGoogleEnvironment(os.Environ(), map[string]string{
		"CLOUDSDK_CONFIG":       configDir,
		"CLOUDSDK_CORE_ACCOUNT": identity.Account,
	})
	environment, chromeProfile, err := configureChromeBrowser(environment, identity.Account, configDir)
	if err != nil {
		return nil, "", err
	}

	command := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "login", identity.Account, "--quiet")
	command.Env = environment
	return command, chromeProfile, nil
}

// CompleteADCLogin installs newly generated credentials at the configured ADC path.
func CompleteADCLogin(identity config.Identity) error {
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return err
	}
	generated := filepath.Join(configDir, "application_default_credentials.json")
	desired, err := adcPath(identity, configDir)
	if err != nil {
		return err
	}
	if generated != desired {
		data, err := os.ReadFile(generated)
		if err != nil {
			return fmt.Errorf("read generated application credentials: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(desired), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(desired, data, 0o600); err != nil {
			return fmt.Errorf("write selected application credentials: %w", err)
		}
	}
	return nil
}

var isolatedGoogleKeys = map[string]bool{
	"CLOUDSDK_ACTIVE_CONFIG_NAME":               true,
	"CLOUDSDK_AUTH_ACCESS_TOKEN":                true,
	"CLOUDSDK_AUTH_ACCESS_TOKEN_FILE":           true,
	"CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE":    true,
	"CLOUDSDK_AUTH_DISABLE_CREDENTIALS":         true,
	"CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT": true,
	"CLOUDSDK_BILLING_QUOTA_PROJECT":            true,
	"CLOUDSDK_CONFIG":                           true,
	"CLOUDSDK_CORE_ACCOUNT":                     true,
	"CLOUDSDK_CORE_PROJECT":                     true,
	"CLOUDSDK_CORE_QUOTA_PROJECT":               true,
	"GOOGLE_APPLICATION_CREDENTIALS":            true,
	"GOOGLE_CLOUD_QUOTA_PROJECT":                true,
}

var adcIdentityEndpoint = "https://openidconnect.googleapis.com/v1/userinfo"
var adcIdentityHTTPClient = http.DefaultClient

func verifyADCIdentity(ctx context.Context, token, expectedAccount string) error {
	if token == "" {
		return fmt.Errorf("application-default authentication returned an empty token for %s", expectedAccount)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, adcIdentityEndpoint, nil)
	if err != nil {
		return fmt.Errorf("prepare ADC identity check: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := adcIdentityHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("verify ADC identity for %s: %w", expectedAccount, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("verify ADC identity for %s: Google identity endpoint returned %s", expectedAccount, response.Status)
	}
	var identity struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&identity); err != nil {
		return fmt.Errorf("verify ADC identity for %s: invalid identity response", expectedAccount)
	}
	if !strings.EqualFold(identity.Email, expectedAccount) {
		observed := identity.Email
		if observed == "" {
			observed = "unknown"
		}
		return fmt.Errorf("ADC identity mismatch: expected %q, observed %q", expectedAccount, observed)
	}
	return nil
}

func isolatedGoogleEnvironment(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		key, _, found := strings.Cut(item, "=")
		if found && isolatedGoogleKeys[key] {
			continue
		}
		result = append(result, item)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

type chromeLocalState struct {
	Profile struct {
		InfoCache map[string]struct {
			Name     string `json:"name"`
			UserName string `json:"user_name"`
		} `json:"info_cache"`
	} `json:"profile"`
}

type chromeProfile struct {
	Directory string
	Name      string
	Account   string
}

func configureChromeBrowser(environment []string, account, configDir string) ([]string, string, error) {
	if runtime.GOOS != "darwin" || os.Getenv("BROWSER") != "" {
		return environment, "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	chromeRoot := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	data, err := os.ReadFile(filepath.Join(chromeRoot, "Local State"))
	if os.IsNotExist(err) {
		return environment, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read Chrome profiles: %w", err)
	}
	var state chromeLocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, "", fmt.Errorf("decode Chrome profiles: %w", err)
	}
	profiles := make([]chromeProfile, 0, len(state.Profile.InfoCache))
	for directory, details := range state.Profile.InfoCache {
		profiles = append(profiles, chromeProfile{Directory: directory, Name: details.Name, Account: details.UserName})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Directory < profiles[j].Directory })
	selected, err := selectChromeProfile(account, profiles)
	if err != nil {
		return nil, "", err
	}
	if selected == nil {
		return environment, "", nil
	}
	chromeExecutable := filepath.Join("/Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome")
	if _, err := os.Stat(chromeExecutable); err != nil {
		return environment, "", nil
	}
	browserLauncher := filepath.Join(configDir, "contexthop-chrome")
	script := "#!/bin/sh\nexec " + shellQuote(chromeExecutable) + " --profile-directory=" + shellQuote(selected.Directory) + " \"$@\"\n"
	if err := os.WriteFile(browserLauncher, []byte(script), 0o700); err != nil {
		return nil, "", fmt.Errorf("create Chrome profile launcher: %w", err)
	}
	return append(environment, "BROWSER="+browserLauncher), selected.Name + " (" + selected.Directory + ")", nil
}

func selectChromeProfile(account string, profiles []chromeProfile) (*chromeProfile, error) {
	for index := range profiles {
		if strings.EqualFold(profiles[index].Account, account) {
			return &profiles[index], nil
		}
	}
	_, domain, hasDomain := strings.Cut(strings.ToLower(account), "@")
	if !hasDomain || domain == "" {
		return nil, nil
	}
	var matches []chromeProfile
	for _, profile := range profiles {
		_, profileDomain, ok := strings.Cut(strings.ToLower(profile.Account), "@")
		if ok && profileDomain == domain {
			matches = append(matches, profile)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.Name+" ("+match.Directory+")")
		}
		return nil, fmt.Errorf("multiple Chrome profiles match domain %s: %s; refusing to choose automatically", domain, strings.Join(names, ", "))
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func adcPath(identity config.Identity, configDir string) (string, error) {
	if strings.TrimSpace(identity.ADC) == "" {
		return filepath.Join(configDir, "application_default_credentials.json"), nil
	}
	return resolver.ExpandPath(identity.ADC)
}

// Logout revokes only the named CLI account in its isolated configuration.
func Logout(ctx context.Context, name string, identity config.Identity) error {
	if identity.Provider != "gcp" {
		return fmt.Errorf("authentication provider %q is not implemented", identity.Provider)
	}
	if strings.TrimSpace(identity.Account) == "" {
		return fmt.Errorf("identity %q has no account", name)
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return err
	}
	if configDir == "" {
		return fmt.Errorf("identity %q has no isolated cloudSdkConfig", name)
	}
	command := exec.CommandContext(ctx, "gcloud", "auth", "revoke", identity.Account, "--quiet")
	command.Env = isolatedGoogleEnvironment(os.Environ(), map[string]string{"CLOUDSDK_CONFIG": configDir, "CLOUDSDK_CORE_ACCOUNT": identity.Account})
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("log out %s: %w: %s", identity.Account, err, strings.TrimSpace(string(output)))
	}
	RecordStatus(identity, "Sign-in required")
	return nil
}
