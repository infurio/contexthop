package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

func consoleURL(r resolver.Resolved, page string) (string, error) {
	if r.Project == nil && r.Identity != nil && r.Identity.Provider == "gcp" && page == "details" {
		return "https://console.cloud.google.com/", nil
	}
	if r.Project == nil || r.Project.Provider != "gcp" {
		return "", errors.New("Console requires a Google Cloud project")
	}
	query := url.Values{"project": {r.Project.ProjectID}}
	path := "/home/dashboard"
	if r.Kubernetes != nil && r.Kubernetes.Type == "gke" {
		k := r.Kubernetes
		if k.Cluster == "" || k.Location == "" {
			return "", errors.New("cluster Console links require saved GKE project, cluster and location metadata; update discovery in chop config")
		}
		switch page {
		case "details":
			path = "/kubernetes/clusters/details/" + url.PathEscape(k.Location) + "/" + url.PathEscape(k.Cluster) + "/details"
		case "workloads":
			path = "/kubernetes/workload/overview" // Project-scoped overview; cluster details are the precise cluster link.
		case "logs":
			path = "/logs/query"
			query.Set("query", "resource.labels.cluster_name="+strconv.Quote(k.Cluster)+"\nresource.labels.location="+strconv.Quote(k.Location)+"\nresource.labels.project_id="+strconv.Quote(r.Project.ProjectID))
		default:
			return "", fmt.Errorf("unknown Console page %q; use details, workloads, or logs", page)
		}
	} else if page != "details" {
		return "", errors.New("workloads and logs shortcuts require a Kubernetes cluster")
	}
	return "https://console.cloud.google.com" + path + "?" + query.Encode(), nil
}

func consoleBrowserCommand(platform string, browser config.BrowserProfile, targetURL string) (string, []string, error) {
	if err := browser.Validate(); err != nil {
		return "", nil, err
	}
	if browser.Profile == "" {
		return "", nil, errors.New("no browser profile configured; open the Identities tab, highlight the account and press b")
	}
	if platform != "darwin" {
		return "", nil, errors.New("profile-aware Console opening currently supports macOS")
	}
	app := "Google Chrome"
	if browser.App == "edge" {
		app = "Microsoft Edge"
	}
	return "open", []string{"-na", app, "--args", "--profile-directory=" + browser.Profile, targetURL}, nil
}

func browserProfilePath(home string, browser config.BrowserProfile) string {
	root := "Google/Chrome"
	if browser.App == "edge" {
		root = "Microsoft Edge"
	}
	return filepath.Join(home, "Library", "Application Support", root, browser.Profile)
}

// Console selection is read-only. Explicit bindings win; automatic selection
// requires exactly one existing profile with the same account, never a domain guess.
func resolveConsoleBrowser(home string, identity config.Identity) (config.BrowserProfile, error) {
	if identity.Browser != (config.BrowserProfile{}) {
		return identity.Browser, identity.Browser.Validate()
	}
	var matches []config.BrowserProfile
	for _, app := range []string{"chrome", "edge"} {
		root := browserProfilePath(home, config.BrowserProfile{App: app})
		data, err := os.ReadFile(filepath.Join(root, "Local State"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return config.BrowserProfile{}, fmt.Errorf("read %s profiles: %w", app, err)
		}
		var local struct {
			Profile struct {
				InfoCache map[string]struct {
					Account string `json:"user_name"`
				} `json:"info_cache"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(data, &local); err != nil {
			return config.BrowserProfile{}, fmt.Errorf("read %s profile metadata: %w", app, err)
		}
		for directory, profile := range local.Profile.InfoCache {
			candidate := config.BrowserProfile{App: app, Profile: directory}
			if identity.Account == "" || !strings.EqualFold(strings.TrimSpace(profile.Account), strings.TrimSpace(identity.Account)) || candidate.Validate() != nil {
				continue
			}
			if info, err := os.Stat(browserProfilePath(home, candidate)); err == nil && info.IsDir() {
				matches = append(matches, candidate)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return config.BrowserProfile{}, errors.New("multiple browser profiles match this identity; choose one in the Identities tab (highlight the account and press b)")
	}
	return config.BrowserProfile{}, errors.New("no browser profile matches this identity; set its profile in the Identities tab (highlight the account and press b)")
}

func prepareConsoleCommand(resolved resolver.Resolved, page string) (*exec.Cmd, error) {
	targetURL, err := consoleURL(resolved, page)
	if err != nil {
		return nil, err
	}
	if resolved.Identity == nil {
		return nil, errors.New("choose an identity before opening Console")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	browser, err := resolveConsoleBrowser(home, *resolved.Identity)
	if err != nil {
		return nil, err
	}
	command, args, err := consoleBrowserCommand(runtime.GOOS, browser, targetURL)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(browserProfilePath(home, browser))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("browser profile %q does not exist; select its directory name from chrome://version or edge://version in the Identities tab (highlight the account and press b)", browser.Profile)
	}
	return exec.Command(command, args...), nil
}

func openConsole(resolved resolver.Resolved, page string) error {
	command, err := prepareConsoleCommand(resolved, page)
	if err != nil {
		return err
	}
	fmt.Printf("Opening web console for %s · identity %s\n", resolved.Name, resolved.Identity.Account)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command.Path, command.Args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open web console: %w %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runConsole(args []string) error {
	page, name := "details", ""
	for index := 0; index < len(args); index++ {
		if args[index] == "--page" && index+1 < len(args) {
			index++
			page = args[index]
			continue
		}
		if name != "" || len(args[index]) > 0 && args[index][0] == '-' {
			return errors.New("usage: chop console [identity-project-workspace-or-cluster] [--page details|workloads|logs]")
		}
		name = args[index]
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var resolved resolver.Resolved
	if name != "" {
		target, err := parseLaunchTarget(cfg, name)
		if err != nil {
			return err
		}
		resolved, err = resolveLaunchChoice(cfg, target)
		if errors.Is(err, errLaunchCancelled) {
			return nil
		}
		if err != nil {
			return err
		}
	} else {
		manifest, err := state.LoadManifest(os.Getenv(state.SessionFileEnv))
		if err != nil {
			return errors.New("no active ContextHop session; supply an identity, project, workspace or cluster name")
		}
		p := manifest.Selection()
		resolved, err = resolver.Components(cfg, resolver.Selection{Name: p.Name, Identity: p.Identity, Project: p.Project, Kubernetes: p.Kubernetes})
		if err != nil {
			return err
		}
		projectID := ""
		if resolved.Project != nil {
			projectID = resolved.Project.ProjectID
		}
		if resolved.Identity == nil || resolved.Identity.Account != manifest.Expected.Identity || projectID != manifest.Expected.Project || resolved.Kubernetes != nil && resolver.KubernetesTargetIdentity(*resolved.Kubernetes, resolved.Project) != manifest.KubernetesTargetIdentity {
			return errors.New("active context differs from the saved catalog; select it again or supply an explicit Console target")
		}
	}
	return openConsole(resolved, page)
}

func runBrowserConfig(args []string) error {
	if len(args) == 0 {
		return runConfig(nil)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(args) != 3 {
		return errors.New("usage: chop config browser <identity> <chrome|edge> <profile-directory>")
	}
	plan, err := catalog.PlanBrowser(cfg, args[0], config.BrowserProfile{App: args[1], Profile: args[2]})
	if err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	return catalog.Apply(path, plan)
}
