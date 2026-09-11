package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

func TestSwitchAbortedReportsUnmanagedContext(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "")
	message := switchAborted("work", errors.New("authentication failed")).Error()
	if !strings.Contains(message, `switch to "work" aborted`) || !strings.Contains(message, "Active context unchanged: unmanaged shell") {
		t.Fatalf("message = %q", message)
	}
}

func TestManagedSessionRejectsCommandsThatWouldNestShells(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "current-context")
	t.Setenv(state.SessionFileEnv, "")
	for _, args := range [][]string{nil, {"-r"}, {"workspace"}, {"shell", "other"}, {"other"}} {
		err := rejectNestedSession(args)
		if err == nil || !strings.Contains(err.Error(), "run `exit`") {
			t.Errorf("rejectNestedSession(%q) = %v", args, err)
		}
	}
	for _, args := range [][]string{{"status"}, {"config"}, {"m"}, {"backup"}, {"restore", "latest"}, {"reset"}, {"discover"}, {"exec", "work", "--", "true"}, {"help"}} {
		if err := rejectNestedSession(args); err != nil {
			t.Errorf("rejectNestedSession(%q) = %v", args, err)
		}
	}
	t.Setenv("CONTEXTHOP_IN_PLACE_SWITCH", "1")
	if err := rejectNestedSession([]string{"workspace"}); err == nil || !strings.Contains(err.Error(), "use `chop` without a path") {
		t.Fatalf("direct binary guidance = %v", err)
	}
	t.Setenv(session.ActivationFileEnv, filepath.Join(t.TempDir(), "activation"))
	if err := rejectNestedSession([]string{"workspace"}); err != nil {
		t.Fatalf("session-local function was rejected: %v", err)
	}
}

func TestParseAuthArgsAcceptsADCBeforeOrAfterIdentity(t *testing.T) {
	for _, args := range [][]string{{"work", "--adc"}, {"--adc", "work"}} {
		name, adc, err := parseAuthArgs(args)
		if err != nil || name != "work" || !adc {
			t.Fatalf("parseAuthArgs(%q) = %q, %t, %v", args, name, adc, err)
		}
	}
}

func TestParseAuthArgsRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"work", "other"}, {"work", "--unknown"}, {"work", "--adc", "--adc"}} {
		if _, _, err := parseAuthArgs(args); err == nil {
			t.Fatalf("parseAuthArgs(%q) accepted invalid arguments", args)
		}
	}
}

func TestParseWriteModeRejectsConflictingAndDuplicateOptions(t *testing.T) {
	for _, args := range [][]string{{"--write", "--dry-run"}, {"--write", "--write"}, {"--dry-run", "--dry-run"}, {"--unknown"}} {
		if _, _, err := parseWriteMode("init", args); err == nil {
			t.Fatalf("parseWriteMode(%q) accepted invalid options", args)
		}
	}
	write, dryRun, err := parseWriteMode("init", []string{"--dry-run"})
	if err != nil || write || !dryRun {
		t.Fatalf("parseWriteMode(--dry-run) = %t, %t, %v", write, dryRun, err)
	}
}

func TestTopLevelCommandsRejectTrailingArgumentsAndUnknownOptions(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "")
	for _, args := range [][]string{{"status", "extra"}, {"list", "extra"}, {"kubernetes", "extra"}, {"catalog", "path", "extra"}, {"debug", "--unknown"}, {"version", "extra"}, {"--unknown"}, {"workspace-name", "extra"}} {
		if err := run(args); err == nil {
			t.Fatalf("run(%q) accepted invalid arguments", args)
		}
	}
}

func TestInteractiveBrowserPickerFiltersDependentResources(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	cfg.Projects["work"] = config.Project{Provider: "gcp", ProjectID: "work-project", Identities: []string{"person"}}
	cfg.Projects["other"] = config.Project{Provider: "gcp", ProjectID: "other-project", Identities: []string{"other"}}
	cfg.Kubernetes["work-cluster"] = config.Kubernetes{Type: "gke", Project: "work", Context: "work-context"}
	cfg.Kubernetes["other-cluster"] = config.Kubernetes{Type: "gke", Project: "other", Context: "other-context"}
	cfg.Destinations["work"] = config.Destination{Identity: "person", Project: "work", Kubernetes: "work-cluster"}
	cfg.Destinations["other"] = config.Destination{Identity: "other", Project: "other", Kubernetes: "other-cluster"}

	identityDraft := ui.Draft{ui.ScreenIdentity: "person"}
	projects := interactiveBrowserPicker(cfg, cfg, ui.ScreenProject, identityDraft)
	if !pickerHasOption(projects, "work", "") || pickerHasOption(projects, "other", "") {
		t.Fatalf("identity-scoped projects = %#v", projects.Options)
	}
	kubernetes := interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, identityDraft)
	if !pickerHasOption(kubernetes, "work-cluster", "") || pickerHasOption(kubernetes, "other-cluster", "") {
		t.Fatalf("identity-scoped Kubernetes = %#v", kubernetes.Options)
	}

	projectDraft := ui.Draft{ui.ScreenIdentity: "person", ui.ScreenProject: "work"}
	kubernetes = interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, projectDraft)
	if !pickerHasOption(kubernetes, "work-cluster", "") || pickerHasOption(kubernetes, "other-cluster", "") {
		t.Fatalf("project-scoped Kubernetes = %#v", kubernetes.Options)
	}
	workspaces := interactiveBrowserPicker(cfg, cfg, ui.ScreenWorkspace, projectDraft)
	if !pickerHasOption(workspaces, "work", "") || !pickerHasOption(workspaces, "other", "") {
		t.Fatalf("workspaces must remain visible = %#v", workspaces.Options)
	}
}

func TestParseGKEArgumentsAcceptsPastedRegionalDNSCommand(t *testing.T) {
	parsed, err := parseGKEArguments("example-sites-dev --region us-central1 --project example-project-123456 --dns-endpoint")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Cluster != "example-sites-dev" || parsed.Location != "us-central1" || parsed.ProjectID != "example-project-123456" || parsed.Endpoint != "dns" {
		t.Fatalf("parsed arguments = %#v", parsed)
	}
	parsed, err = parseGKEArguments("gcloud container clusters get-credentials example-sites-dev --zone=us-central1-a --project=example-project-123456 --internal-ip --quiet")
	if err != nil || parsed.Location != "us-central1-a" || parsed.Endpoint != "internal-ip" {
		t.Fatalf("full command = %#v, %v", parsed, err)
	}
}

func TestParseGKEArgumentsRejectsIncompleteAndConflictingInput(t *testing.T) {
	for _, input := range []string{
		"example-sites-dev --project example-project-123456",
		"example-sites-dev --region us-central1 --project example-project-123456 --dns-endpoint --internal-ip",
	} {
		if _, err := parseGKEArguments(input); err == nil {
			t.Fatalf("accepted invalid arguments %q", input)
		}
	}
}

func TestGKEAddFlowStagesPastedCommandAndIdentityMapping(t *testing.T) {
	cfg := config.New()
	cfg.Identities["team-a"] = config.Identity{Provider: "gcp", Account: "developer@team-a.example.com"}
	cfg.Projects["example-project"] = config.Project{Provider: "gcp", ProjectID: "example-project-123456"}
	cfg.Kubernetes["imported"] = config.Kubernetes{Type: "gke", Project: "example-project", Cluster: "example-sites-dev", Location: "us-central1", Kubeconfig: "/config", Context: "imported"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	draft := ui.Draft{ui.ScreenCatalogAction: "add-kubernetes"}

	transition := flow(ui.Choice{Screen: ui.ScreenDependencyTarget, Option: ui.Option{Name: "gke"}}, draft)
	if transition.Picker.Screen != screenAddKubeIdentity {
		t.Fatalf("identity step = %#v", transition.Picker)
	}
	transition = flow(ui.Choice{Screen: screenAddKubeIdentity, Option: ui.Option{Name: "team-a"}}, draft)
	if transition.Picker.Screen != screenAddKubeProject || transition.Picker.Options[0].Name != "\x00__paste__" {
		t.Fatalf("project step = %#v", transition.Picker)
	}
	transition = flow(ui.Choice{Screen: screenAddKubeProject, Option: ui.Option{Name: "\x00__paste__"}}, draft)
	if transition.Picker.Screen != screenAddKubeCommand {
		t.Fatalf("paste step = %#v", transition.Picker)
	}
	transition = flow(ui.Choice{Screen: screenAddKubeCommand, Option: ui.Option{Name: "example-sites-dev --region us-central1 --project example-project-123456 --dns-endpoint"}}, draft)
	if transition.Picker.Screen != screenAddKubeReview || !strings.Contains(transition.Picker.Description, "example-sites-dev-dns") || !strings.Contains(transition.Picker.Description, "will be mapped") {
		t.Fatalf("review step = %#v", transition.Picker)
	}
	transition = flow(ui.Choice{Screen: screenAddKubeReview, Option: ui.Option{Name: "save"}}, draft)
	if transition.Picker.Screen != ui.ScreenConfirm || editor.plan == nil || !editor.plan.Valid() {
		t.Fatalf("confirmation = %#v, plan = %#v", transition.Picker, editor.plan)
	}
	if !strings.Contains(transition.Picker.Description, "identity:developer@team-a.example.com [team-a]") || !strings.Contains(transition.Picker.Description, "endpoint:dns") {
		t.Fatalf("confirmation description = %q", transition.Picker.Description)
	}
	got := editor.plan.Config.Kubernetes["example-sites-dev-dns"]
	if got.Endpoint != "dns" || got.PreferredIdentity != "team-a" || !slices.Contains(editor.plan.Config.Projects["example-project"].Identities, "team-a") {
		t.Fatalf("staged config = %#v / %#v", got, editor.plan.Config.Projects["example-project"])
	}
}

func TestCatalogPostApplyKeepsEditsAtEditedEntity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project"}
	plan, err := catalog.PlanMapIdentity(cfg, "project", "person")
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	draft := ui.Draft{
		ui.ScreenCatalogEntity: catalogCategoryPrefix + "identities",
		screenCatalogList:      encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "person"}),
		screenCatalogIdentity:  "identity-map-project",
	}
	got := catalogPostApplyDestination(plan, draft)
	if got.Entity != "identity:person" || got.Category != "" || got.Notice != "Identity updated successfully." {
		t.Fatalf("post-apply destination = %#v", got)
	}
}

func TestCatalogPostApplyReturnsDeletedEntityToItsCategory(t *testing.T) {
	cfg := config.New()
	cfg.Identities["developer"] = config.Identity{Provider: "gcp", Account: "developer@example.com"}
	plan, err := catalog.PlanForget(cfg, catalog.Ref{Kind: catalog.KindIdentity, Name: "developer"})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	draft := ui.Draft{
		ui.ScreenCatalogEntity: catalogCategoryPrefix + "identities",
		screenCatalogList:      encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "developer"}),
	}
	got := catalogPostApplyDestination(plan, draft)
	if got.Entity != "" || got.Category != "identities" || got.Notice != "Identity forgotten successfully." {
		t.Fatalf("post-apply destination = %#v", got)
	}
}

func TestCatalogPostApplyKeepsVisibilityChangesLocal(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	ref := catalog.Ref{Kind: catalog.KindIdentity, Name: "person"}
	hide, err := catalog.PlanSetHidden(cfg, ref, true)
	if err != nil || !hide.Valid() {
		t.Fatalf("hide plan = %#v, %v", hide, err)
	}
	draft := ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "identities", screenCatalogList: encodeCatalogRef(ref)}
	if got := catalogPostApplyDestination(hide, draft); got.Category != "" || got.Entity != encodeCatalogRef(ref) || got.Notice != "Identity hidden successfully." {
		t.Fatalf("hide destination = %#v", got)
	}
	unhide, err := catalog.PlanSetHidden(hide.Config, ref, false)
	if err != nil || !unhide.Valid() {
		t.Fatalf("unhide plan = %#v, %v", unhide, err)
	}
	if got := catalogPostApplyDestination(unhide, ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "hidden", screenCatalogList: encodeCatalogRef(ref)}); got.Category != "" || got.Entity != encodeCatalogRef(ref) || got.Notice != "Identity unhidden successfully." {
		t.Fatalf("unhide destination = %#v", got)
	}
	deleted, err := catalog.PlanRemove(hide.Config, ref)
	if err != nil || !deleted.Valid() {
		t.Fatalf("delete plan = %#v, %v", deleted, err)
	}
	if got := catalogPostApplyDestination(deleted, ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "hidden", screenCatalogList: encodeCatalogRef(ref)}); got.Category != "hidden" || got.Entity != "" || got.Notice != "Identity deleted successfully." {
		t.Fatalf("hidden delete destination = %#v", got)
	}
}

func TestCatalogPostApplyReturnsNestedDeletionToParentEntity(t *testing.T) {
	cfg := config.New()
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}
	plan, err := catalog.PlanForget(cfg, catalog.Ref{Kind: catalog.KindKubernetes, Name: "cluster"})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	draft := ui.Draft{
		screenCatalogList:    encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "project"}),
		screenCatalogProject: encodeCatalogRef(catalog.Ref{Kind: catalog.KindKubernetes, Name: "cluster"}),
	}
	got := catalogPostApplyDestination(plan, draft)
	if got.Entity != "project:project" || got.Category != "" || got.Notice != "Kubernetes target removed successfully." {
		t.Fatalf("post-apply destination = %#v", got)
	}
}

func TestCatalogPostApplyFocusesEveryNewResourceKind(t *testing.T) {
	workspaceCfg := config.New()
	workspaceCfg.Docker["local"] = config.Docker{Context: "local"}
	kubernetesCfg := config.New()
	kubernetesCfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project"}
	identityPlan, identityErr := catalog.PlanAddIdentity(config.New(), "person", config.Identity{Provider: "gcp", Account: "person@example.com"})
	projectPlan, projectErr := catalog.PlanAddProject(config.New(), "project", config.Project{Provider: "gcp", ProjectID: "project"})
	kubernetesPlan, kubernetesErr := catalog.PlanAddKubernetes(kubernetesCfg, "cluster", config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"})
	dockerPlan, dockerErr := catalog.PlanAddDocker(config.New(), "local", config.Docker{Context: "local"})
	workspacePlan, workspaceErr := catalog.PlanAddWorkspace(workspaceCfg, "work", config.Destination{Docker: "local"})
	tests := []struct {
		name string
		plan catalog.Plan
		want string
	}{
		{name: "identity", plan: mustCatalogPlan(t, identityPlan, identityErr), want: "identity:person"},
		{name: "project", plan: mustCatalogPlan(t, projectPlan, projectErr), want: "project:project"},
		{name: "kubernetes", plan: mustCatalogPlan(t, kubernetesPlan, kubernetesErr), want: "kubernetes:cluster"},
		{name: "docker", plan: mustCatalogPlan(t, dockerPlan, dockerErr), want: "docker:local"},
		{name: "workspace", plan: mustCatalogPlan(t, workspacePlan, workspaceErr), want: "workspace:work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := catalogPostApplyDestination(test.plan, ui.Draft{ui.ScreenCatalogEntity: catalogAddSelection})
			if got.Entity != test.want || got.Category != "" {
				t.Fatalf("post-apply destination = %#v", got)
			}
		})
	}
}

func mustCatalogPlan(t *testing.T, plan catalog.Plan, err error) catalog.Plan {
	t.Helper()
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	return plan
}

func TestUnresolvedKubernetesOptionExplainsMissingIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Projects["work"] = config.Project{Provider: "gcp", ProjectID: "work-project"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "work", Context: "Work Cluster"}
	cfg.Destinations["cluster"] = config.Destination{Kubernetes: "cluster"}
	option, ok := unresolvedKubernetesOption(cfg, "cluster", "kubernetes")
	if !ok {
		t.Fatal("unresolved Kubernetes workspace was hidden")
	}
	if option.KubernetesContext != "Work Cluster" || option.ProjectID != "work-project" || option.IdentityAccount != "not mapped" {
		t.Fatalf("option = %#v", option)
	}
}

func TestUnmappedKubernetesSelectionPromptsForIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["personal"] = config.Identity{Provider: "gcp", Account: "personal@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example-project"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	transition := flow(ui.Choice{Screen: ui.ScreenKubernetes, Option: ui.Option{Name: "cluster"}}, ui.Draft{ui.ScreenKubernetes: "cluster"})
	if transition.Picker.Screen != ui.ScreenIdentity || len(transition.Picker.Options) != 2 {
		t.Fatalf("identity prompt = %#v", transition.Picker)
	}
	if !strings.Contains(transition.Picker.Description, "no saved identity mapping") {
		t.Fatalf("identity prompt description = %q", transition.Picker.Description)
	}
	completed := flow(ui.Choice{Screen: ui.ScreenIdentity, Option: ui.Option{Name: "work"}}, ui.Draft{ui.ScreenKubernetes: "cluster", ui.ScreenIdentity: "work"})
	if !completed.Complete {
		t.Fatalf("identity selection = %#v", completed)
	}

	selection, err := selectionFromDraft(cfg, ui.Draft{ui.ScreenKubernetes: "cluster", ui.ScreenIdentity: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Project != "project" || selection.Identity != "work" {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestSwitchAbortedReportsExistingManagedContext(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "team-a-dev")
	message := switchAborted("team-a-dev", errors.New("authentication failed")).Error()
	if !strings.Contains(message, "Active context unchanged: team-a-dev (it was already active before this attempt)") {
		t.Fatalf("message = %q", message)
	}
}

func TestIdentityOptionsKeepDistinctProfiles(t *testing.T) {
	cfg := config.New()
	cfg.Identities["team-a"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Identities["team-a-copy"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Identities["personal"] = config.Identity{Provider: "gcp", Account: "personal@example.net"}
	cfg.Destinations["one"] = config.Destination{Identity: "team-a"}
	cfg.Destinations["two"] = config.Destination{Identity: "team-a"}

	options := identityOptions(cfg, nil)
	if len(options) != 3 {
		t.Fatalf("identity options = %#v, want three profiles", options)
	}
	if options[0].IdentityAccount != "personal@example.net" || options[1].IdentityAccount != "person@example.com [team-a]" || options[2].IdentityAccount != "person@example.com [team-a-copy]" {
		t.Fatalf("identity options = %#v", options)
	}
}

func TestProjectOptionsOnlyIncludeCompatibleIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["work-alias"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["personal"] = config.Identity{Provider: "gcp", Account: "personal@example.net"}
	cfg.Projects["compatible"] = config.Project{Provider: "gcp", ProjectID: "compatible-id", Identities: []string{"work-alias"}}
	cfg.Projects["other"] = config.Project{Provider: "gcp", ProjectID: "other-id", Identities: []string{"personal"}}

	options := projectOptions(cfg, "work-alias")
	if len(options) != 1 || options[0].Name != "compatible" {
		t.Fatalf("project options = %#v", options)
	}
	if got := mappedIdentityForProject(cfg, "compatible", "work"); got != "" {
		t.Fatalf("must not substitute work-alias for work: %q", got)
	}
}

func TestSessionProjectPickerUsesSharedDiscovery(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["configured"] = config.Project{Provider: "gcp", ProjectID: "configured", Identities: []string{"work"}}

	options := sessionProjectOptions(cfg, "work")
	if len(options) != 1 || options[0].Name != "configured" {
		t.Fatalf("project options = %#v", options)
	}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	transition := flow(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: searchProjectsSelection}}, ui.Draft{ui.ScreenProject: searchProjectsSelection})
	if transition.Picker.Screen != screenProjectSearchIdentity || len(transition.Picker.Options) != 1 {
		t.Fatalf("direct project search = %#v", transition.Picker)
	}
}

func TestProjectSearchAuthenticationFailureOffersRecovery(t *testing.T) {
	for _, err := range []error{
		errors.New("gcloud projects list: account does not have any valid credentials; run gcloud auth login"),
		errors.New("Reauthentication failed. cannot prompt during non-interactive execution"),
	} {
		if !projectRefreshNeedsAuthentication(err) {
			t.Fatalf("expected gcloud credential failure to require authentication: %v", err)
		}
	}

	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	choice := ui.Choice{Screen: screenProjectSearchResults, Option: ui.Option{Name: authenticateProjectSearchPrefix + "work"}}
	if transition := flow(choice, ui.Draft{}); transition.Picker.Screen != screenProviderAuth {
		t.Fatalf("authentication transition = %#v", transition)
	}
}

func TestGKEClusterAuthenticationFailureOffersRecoveryAndManualEntry(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	fakeGcloud := "#!/bin/sh\necho 'Reauthentication failed. cannot prompt during non-interactive execution' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gcloud"), []byte(fakeGcloud), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Identities["team-a"] = config.Identity{Provider: "gcp", Account: "developer@team-a.example.com", CloudSDKConfig: t.TempDir()}
	cfg.Projects["example-project"] = config.Project{Provider: "gcp", ProjectID: "example-project-123456"}
	transition := catalogClusterDiscoveryPickerAuthenticated(cfg, "example-project", "team-a")
	options := transition.Picker.Options
	if len(options) != 2 || options[0].Name != authenticateGKEPrefix+"team-a" || options[1].Name != "\x00__manual__" {
		t.Fatalf("recovery options = %#v", options)
	}
	if !strings.Contains(transition.Picker.Description, "Authentication expired") || strings.Contains(transition.Picker.Description, "exit status") {
		t.Fatalf("recovery description = %q", transition.Picker.Description)
	}
	editor := &catalogEditorState{gke: &gkeEditorDraft{Identity: "team-a", Project: "example-project"}}
	flow := interactiveFlow(cfg, cfg, editor)
	choice := ui.Choice{Screen: screenAddKubeChoice, Option: options[0]}
	result := flow(choice, ui.Draft{ui.ScreenCatalogAction: "add-kubernetes"})
	if result.Picker.Screen != screenProviderAuth {
		t.Fatalf("authentication transition = %#v", result)
	}
	t.Setenv("BROWSER", "true")
	execute := flow(ui.Choice{Screen: screenProviderAuth, Option: result.Picker.Options[0]}, ui.Draft{})
	if execute.Process == nil || execute.Process.Command == nil || execute.Process.Done == nil {
		t.Fatalf("authentication process = %#v", execute)
	}
}

func TestComponentContextNameUsesMostSpecificSelection(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster-id", Context: "Friendly Cluster"}

	name := componentContextName(cfg, resolver.Selection{Identity: "work", Project: "project", Kubernetes: "cluster"})
	if name != "Friendly Cluster" {
		t.Fatalf("context name = %q", name)
	}
}

func TestKubernetesOptionsCollapseGKEAliasesAndPreferFriendlyName(t *testing.T) {
	cfg := config.New()
	cfg.Projects["production"] = config.Project{Provider: "gcp", ProjectID: "example-prod-1234", Risk: "production"}
	cfg.Kubernetes["generated"] = config.Kubernetes{
		Type: "gke", Project: "production", Location: "us-central1", Cluster: "acme-fixture-2",
		Context: "gke_example-prod-1234_us-central1_acme-fixture-2", Risk: "production",
	}
	cfg.Kubernetes["friendly"] = config.Kubernetes{
		Type: "gke", Project: "production", Location: "us-central1", Cluster: "acme-fixture-2",
		Context: "EXAMPLE-Prod", Risk: "production",
	}

	options := kubernetesOptions(cfg, "")
	if len(options) != 1 {
		t.Fatalf("options = %#v, want one physical cluster", options)
	}
	if options[0].Name != "friendly" || options[0].KubernetesCluster != "acme-fixture-2" || options[0].KubernetesContext != "EXAMPLE-Prod" || options[0].KubernetesLocation != "us-central1" {
		t.Fatalf("option = %#v, want friendly alias", options[0])
	}
	if summary := kubernetesCountSummary(cfg, "production"); summary != "1 cluster" {
		t.Fatalf("cluster count = %q", summary)
	}
}

func TestKubernetesOptionsKeepDistinctAccessProfilesForOneCluster(t *testing.T) {
	cfg := config.New()
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example"}
	cfg.Kubernetes["developer"] = config.Kubernetes{
		Type: "gke", Project: "project", Location: "us-east1", Cluster: "cluster", Context: "Developer", AccessID: "developer-access",
	}
	cfg.Kubernetes["administrator"] = config.Kubernetes{
		Type: "gke", Project: "project", Location: "us-east1", Cluster: "cluster", Context: "Administrator", AccessID: "administrator-access",
	}

	options := kubernetesOptions(cfg, "")
	if len(options) != 2 {
		t.Fatalf("distinct access profiles were hidden: %#v", options)
	}
}

func TestKubernetesSelectionUsesConfirmedIdentityPreference(t *testing.T) {
	cfg := config.New()
	cfg.Identities["first"] = config.Identity{Provider: "gcp", Account: "first@example.com"}
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"first", "second"}, PreferredIdentity: "first"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1", PreferredIdentity: "second"}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	transition := flow(ui.Choice{Screen: ui.ScreenKubernetes, Option: ui.Option{Name: "cluster"}}, ui.Draft{ui.ScreenKubernetes: "cluster"})
	if !transition.Complete {
		t.Fatalf("confirmed preference did not resolve selection: %#v", transition)
	}
	selection, err := selectionFromDraft(cfg, ui.Draft{ui.ScreenKubernetes: "cluster"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Identity != "second" || selection.Project != "project" {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestKubernetesOptionsExposePlainPhysicalClusterAndContextAlias(t *testing.T) {
	cfg := config.New()
	cfg.Kubernetes["friendly"] = config.Kubernetes{
		Type: "kubeconfig", Kubeconfig: "/configs/team.yaml",
		Cluster: "prod-api", Context: "Friendly-Prod",
	}
	options := kubernetesOptions(cfg, "")
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	option := options[0]
	if option.KubernetesCluster != "prod-api" || option.KubernetesContext != "Friendly-Prod" || option.KubernetesLocation != "" {
		t.Fatalf("option = %#v", option)
	}
}

func TestKubernetesPhysicalNameFallback(t *testing.T) {
	if got := kubernetesPhysicalName("target", config.Kubernetes{Context: "Friendly"}); got != "Friendly" {
		t.Fatalf("context fallback = %q", got)
	}
	if got := kubernetesPhysicalName("target", config.Kubernetes{}); got != "target" {
		t.Fatalf("target fallback = %q", got)
	}
}

func TestKubernetesContextDisplayDisambiguatesConflictingSources(t *testing.T) {
	cfg := config.New()
	first := config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/configs/team-a.yaml", Context: "production"}
	second := config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/configs/team-b.yaml", Context: "production"}
	cfg.Kubernetes["production-team-a"] = first
	cfg.Kubernetes["production-team-b"] = second

	if got := kubernetesContextDisplay(cfg, "production-team-a", first, first.Context); got != "production [team-a.yaml]" {
		t.Fatalf("display = %q", got)
	}
}

func TestInteractiveFlowBuildsOneReversibleDependencyPath(t *testing.T) {
	cfg := config.New()
	cfg.Identities["first"] = config.Identity{Provider: "gcp", Account: "first@example.com"}
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	cfg.Projects["work"] = config.Project{Provider: "gcp", ProjectID: "work-id", Identities: []string{"first", "second"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "work", Cluster: "cluster"}
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})

	project := flow(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: "work"}}, ui.Draft{ui.ScreenProject: "work"})
	if project.Complete || project.Picker.Screen != ui.ScreenIdentity || len(project.Picker.Options) != 2 {
		t.Fatalf("project transition = %#v", project)
	}
	identity := flow(ui.Choice{Screen: ui.ScreenIdentity, Option: ui.Option{Name: "second"}}, ui.Draft{ui.ScreenProject: "work", ui.ScreenIdentity: "second"})
	if identity.Complete || identity.Picker.Screen != ui.ScreenKubernetes {
		t.Fatalf("identity transition = %#v", identity)
	}
	if len(identity.Picker.Options) != 2 || identity.Picker.Options[1].Name != noKubernetesSelection {
		t.Fatalf("Kubernetes options = %#v", identity.Picker.Options)
	}
}

func TestSelectionFromDraftDerivesAndValidatesDependencies(t *testing.T) {
	cfg := config.New()
	cfg.Identities["friendly"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Identities["mapped"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["work"] = config.Project{Provider: "gcp", ProjectID: "work-id", Identities: []string{"mapped"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "work", Cluster: "cluster"}

	if _, err := selectionFromDraft(cfg, ui.Draft{ui.ScreenKubernetes: "cluster", ui.ScreenIdentity: "friendly"}); err == nil {
		t.Fatal("must not substitute credential profiles")
	}
	selection, err := selectionFromDraft(cfg, ui.Draft{ui.ScreenKubernetes: "cluster", ui.ScreenIdentity: "mapped"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Kubernetes != "cluster" || selection.Project != "work" || selection.Identity != "mapped" {
		t.Fatalf("selection = %#v", selection)
	}

	cfg.Projects["unmapped"] = config.Project{Provider: "gcp", ProjectID: "unmapped-id"}
	if _, err := selectionFromDraft(cfg, ui.Draft{ui.ScreenProject: "unmapped"}); err == nil || !strings.Contains(err.Error(), "no discovered identity") {
		t.Fatalf("unmapped project error = %v", err)
	}
}

func TestCatalogMappingFlowAppliesWithoutSecondConfirmation(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["payments"] = config.Project{Provider: "gcp", ProjectID: "payments"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)

	entity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "payments"})
	browse := flow(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: entity}}, ui.Draft{screenCatalogList: entity})
	if browse.Picker.Screen != screenCatalogProject {
		t.Fatalf("entity transition = %#v", browse)
	}
	target := flow(ui.Choice{Screen: screenCatalogProject, Option: ui.Option{Name: "project-map-identity"}}, ui.Draft{screenCatalogList: entity, screenCatalogProject: "project-map-identity"})
	if target.Picker.Screen != ui.ScreenDependencyTarget || target.Picker.Title != "Catalog › Projects › payments › Map identity" || !pickerHasOption(target.Picker, "work", "work@example.com") {
		t.Fatalf("action transition = %#v", target)
	}
	if target.Picker.Options[0].Detail != "work" {
		t.Fatalf("identity option should retain local key as secondary detail: %#v", target.Picker.Options[0])
	}
	preview := flow(ui.Choice{Screen: ui.ScreenDependencyTarget, Option: ui.Option{Name: "work"}}, ui.Draft{
		screenCatalogList: entity, screenCatalogProject: "project-map-identity", ui.ScreenDependencyTarget: "work",
	})
	if !preview.Complete || !editor.applyImmediately || editor.plan == nil || !editor.plan.Valid() {
		t.Fatalf("preview = %#v, editor = %#v", preview, editor)
	}
	if got := editor.plan.Config.Projects["payments"].Identities; len(got) != 1 || got[0] != "work" {
		t.Fatalf("planned identities = %#v", got)
	}
	if len(cfg.Projects["payments"].Identities) != 0 {
		t.Fatal("mapping flow mutated the source configuration while preparing the change")
	}
}

func TestCatalogPlanDescriptionShowsConcreteChangesAndWorkspaceImpact(t *testing.T) {
	plan := catalog.Plan{
		Changes: []catalog.Change{{Action: "add", To: catalog.Ref{Kind: catalog.KindIdentity, Name: "work"}}},
		Impacts: []catalog.Impact{{
			Workspace: "payments",
			Before:    catalog.WorkspaceState{Valid: true, Project: "dev", Risk: "development"},
			After:     catalog.WorkspaceState{Valid: true, Project: "prod", Risk: "production"},
		}},
	}
	description := catalogPlanDescription(plan, nil)
	for _, want := range []string{"add identity:work", "workspace payments: project:dev → project:prod"} {
		if !strings.Contains(description, want) {
			t.Fatalf("description missing %q: %s", want, description)
		}
	}
}

func TestGuidedManualIdentityAdditionCarriesProvenance(t *testing.T) {
	cfg := config.New()
	editor := &catalogEditorState{}
	transition := catalogAddStep(cfg, ui.Choice{Screen: screenAddIdentityAccount, Option: ui.Option{Name: "person@example.com"}}, ui.Draft{
		ui.ScreenCatalogAction: "add-identity", screenAddIdentityName: "work",
	}, editor)
	if !transition.Complete || !editor.applyImmediately || editor.plan == nil || !editor.plan.Valid() {
		t.Fatalf("transition = %#v, editor = %#v", transition, editor)
	}
	identity := editor.plan.Config.Identities["work"]
	if identity.Account != "person@example.com" || identity.Provenance != "manual" || identity.CloudSDKConfig == "" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestWorkspaceCreationComposesAndConfirmsOnce(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["dev"] = config.Project{Provider: "gcp", ProjectID: "example-development", Identities: []string{"work"}, Risk: "development"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "dev", Cluster: "development", Location: "us-east1"}
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	editor := &catalogEditorState{}

	start := catalogAddTransition(cfg, "add-workspace", nil, editor)
	if editor.workspace == nil || start.Picker.Screen != screenAddWorkspaceKind || !start.Picker.HideSearch {
		t.Fatalf("workspace start = %#v, editor = %#v", start, editor)
	}
	targets := catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceKind, Option: ui.Option{Name: string(catalog.KindKubernetes)}}, nil, editor)
	if targets.Picker.Screen != screenAddWorkspaceTarget || !strings.Contains(targets.Picker.Options[0].Detail, "example-development") || !strings.Contains(targets.Picker.Options[0].Detail, "us-east1") {
		t.Fatalf("workspace targets lack context: %#v", targets.Picker)
	}
	builder := catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceTarget, Option: ui.Option{Name: "cluster"}}, nil, editor)
	if !builder.ReturnToPrevious || builder.Picker.Screen != screenAddWorkspaceBuilder || editor.plan != nil {
		t.Fatalf("workspace builder = %#v, editor = %#v", builder, editor)
	}
	for _, want := range []string{"development · explicit", "example-development · inferred", "person@example.com · inferred"} {
		if !pickerContainsDetail(builder.Picker, want) {
			t.Fatalf("workspace summary missing %q: %#v", want, builder.Picker.Options)
		}
	}

	dockerPicker := catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-set:docker"}}, nil, editor)
	if dockerPicker.Picker.Screen != screenAddWorkspaceComponent || !pickerHasOption(dockerPicker.Picker, "", "No docker") {
		t.Fatalf("docker edit = %#v", dockerPicker.Picker)
	}
	builder = catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceComponent, Option: ui.Option{Name: "local"}}, nil, editor)
	if !builder.ReturnToPrevious || !pickerContainsDetail(builder.Picker, "desktop-linux · explicit") {
		t.Fatalf("updated workspace builder = %#v", builder)
	}

	editor.workspace.Value.ADC = "identity"
	confirm := catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}}, nil, editor)
	if confirm.Picker.Screen != ui.ScreenConfirm || editor.plan == nil || !editor.plan.Valid() || !pickerHasOption(confirm.Picker, "apply", "Add workspace") {
		t.Fatalf("workspace confirmation = %#v, editor = %#v", confirm, editor)
	}
	workspace := editor.plan.Config.Destinations["cluster"]
	if workspace.Kubernetes != "cluster" || workspace.Docker != "local" || workspace.ADC != "identity" || workspace.Identity != "" || workspace.Project != "" {
		t.Fatalf("composed workspace = %#v", workspace)
	}
}

func TestWorkspacePickerContainsOnlySavedWorkspaces(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["existing"] = config.Destination{Docker: "local", Hidden: true}
	options := workspacePickerOptions(cfg)
	if len(options) != 1 || options[0].Name != "existing" {
		t.Fatalf("workspace options = %#v", options)
	}
}

func TestExistingWorkspaceStagesMultipleChangesBeforeOneConfirmation(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["dev"] = config.Project{Provider: "gcp", ProjectID: "example-development", Identities: []string{"work"}}
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["example-project"] = config.Destination{Identity: "work", Provenance: "manual"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	encoded := encodeCatalogRef(catalog.Ref{Kind: catalog.KindWorkspace, Name: "example-project"})

	builder := flow(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: encoded}}, ui.Draft{screenCatalogList: encoded})
	if builder.Picker.Screen != screenAddWorkspaceBuilder || editor.workspace == nil || !editor.workspace.Existing || pickerHasOption(builder.Picker, "workspace-save", "Save workspace") {
		t.Fatalf("existing workspace builder = %#v, editor = %#v", builder, editor)
	}
	if pickerHasOption(builder.Picker, "workspace-hide", "Hide workspace") || !pickerHasOption(builder.Picker, "workspace-delete", "Delete from ContextHop") {
		t.Fatalf("workspace visibility actions = %#v", builder.Picker.Options)
	}
	projectPicker := flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-set:project"}}, nil)
	if !pickerHasOption(projectPicker.Picker, "dev", "example-development") {
		t.Fatalf("project picker = %#v", projectPicker.Picker)
	}
	builder = flow(ui.Choice{Screen: screenAddWorkspaceComponent, Option: ui.Option{Name: "dev"}}, nil)
	if !builder.ReturnToPrevious || !pickerHasOption(builder.Picker, "workspace-save", "Save workspace") {
		t.Fatalf("builder after project edit = %#v", builder)
	}
	flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-set:docker"}}, nil)
	builder = flow(ui.Choice{Screen: screenAddWorkspaceComponent, Option: ui.Option{Name: "local"}}, nil)
	if !pickerContainsDetail(builder.Picker, "desktop-linux · explicit") {
		t.Fatalf("builder after Docker edit = %#v", builder)
	}

	confirm := flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-save"}}, nil)
	if confirm.Picker.Screen != ui.ScreenConfirm || editor.plan == nil || !editor.plan.Valid() || len(editor.plan.Changes) != 2 {
		t.Fatalf("staged workspace confirmation = %#v, plan = %#v", confirm, editor.plan)
	}
	updated := editor.plan.Config.Destinations["example-project"]
	if updated.Project != "dev" || updated.Docker != "local" || updated.Identity != "work" {
		t.Fatalf("staged workspace = %#v", updated)
	}
}

func pickerContainsDetail(picker ui.Picker, fragment string) bool {
	for _, option := range picker.Options {
		if strings.Contains(option.Detail, fragment) {
			return true
		}
	}
	return false
}

func TestCatalogOptionsShowCompactCategories(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", Provenance: "manual", VerifiedBy: "gcp", ObservedAt: "2026-09-03T12:00:00Z"}
	cfg.Identities["unmapped"] = config.Identity{Provider: "gcp", Account: "unmapped@example.com"}
	cfg.Projects["payments"] = config.Project{Provider: "gcp", ProjectID: "payments", Identities: []string{"work"}}
	cfg.Projects["missing"] = config.Project{Provider: "gcp", ProjectID: "missing"}
	cfg.Kubernetes["api"] = config.Kubernetes{Type: "gke", Project: "payments", Cluster: "api", Location: "us-central1"}
	cfg.Kubernetes["standalone"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/kubeconfig", Context: "standalone"}
	cfg.Docker["local"] = config.Docker{Context: "default"}
	cfg.Destinations["payments"] = config.Destination{Identity: "work", Project: "payments", Kubernetes: "api"}
	options := catalogEntityOptions(cfg)
	joined := ""
	for _, option := range options {
		joined += option.Label + " " + option.Detail + "\n"
	}
	for _, want := range []string{
		"Identities 2 accounts",
		"Projects 2 projects · 1 needs identity",
		"Kubernetes 2 targets · 1 standalone",
		"Docker 1 target · no cloud dependency",
		"Workspaces 1 workspace",
		"Hidden 0 resources",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("catalog options missing %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"work@example.com", "unmapped@example.com", "project:payments", "workspace:payments"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("catalog options contain noisy detail %q:\n%s", unwanted, joined)
		}
	}
	if projects := catalogCategoryPicker(cfg, "projects"); len(projects.Options) != 2 {
		t.Fatalf("project category omitted mapped resources: %#v", projects.Options)
	}
	if kubernetes := catalogCategoryPicker(cfg, "kubernetes"); len(kubernetes.Options) != 2 {
		t.Fatalf("Kubernetes category omitted project-backed resources: %#v", kubernetes.Options)
	}
}

func TestCatalogSummariesDistinguishNoneFromMissingMappings(t *testing.T) {
	cfg := config.New()
	cfg.Projects["missing"] = config.Project{Provider: "gcp", ProjectID: "missing"}
	cfg.Kubernetes["standalone"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/kubeconfig", Context: "standalone"}
	cfg.Docker["local"] = config.Docker{Context: "default"}

	if got := catalogNodeSummary(cfg, catalog.Ref{Kind: catalog.KindProject, Name: "missing"}); got != "Needs identity" {
		t.Fatalf("missing project mapping = %q", got)
	}
	if got := catalogNodeSummary(cfg, catalog.Ref{Kind: catalog.KindKubernetes, Name: "standalone"}); got != "No project" {
		t.Fatalf("standalone Kubernetes mapping = %q", got)
	}
	if got := catalogNodeSummary(cfg, catalog.Ref{Kind: catalog.KindDocker, Name: "local"}); got != "No cloud dependency" {
		t.Fatalf("Docker dependency = %q", got)
	}
}

func TestCatalogActionMenusExposeUnmapping(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example-project", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "kubeconfig", Project: "project", Context: "cluster"}
	cfg.Docker["docker"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["workspace"] = config.Destination{Identity: "work", Project: "project", Kubernetes: "cluster", Docker: "docker"}

	checks := []struct {
		ref   catalog.Ref
		label string
	}{
		{catalog.Ref{Kind: catalog.KindIdentity, Name: "work"}, "Hide"},
		{catalog.Ref{Kind: catalog.KindProject, Name: "project"}, "Map"},
		{catalog.Ref{Kind: catalog.KindKubernetes, Name: "cluster"}, "Map"},
		{catalog.Ref{Kind: catalog.KindDocker, Name: "docker"}, "Unmap from workspace"},
		{catalog.Ref{Kind: catalog.KindWorkspace, Name: "workspace"}, "Unmap kubernetes"},
	}
	for _, check := range checks {
		picker := catalogActionPicker(cfg, encodeCatalogRef(check.ref))
		found := false
		for _, option := range picker.Options {
			found = found || option.Label == check.label
		}
		if !found {
			t.Errorf("%s actions do not contain %q: %#v", check.ref.Kind, check.label, picker.Options)
		}
	}
}

func TestCatalogActionMenusHideAndDeleteEveryResource(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example-project"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "kubeconfig", Context: "cluster"}
	cfg.Docker["docker"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["workspace"] = config.Destination{Docker: "docker"}

	for _, ref := range []catalog.Ref{
		{Kind: catalog.KindIdentity, Name: "work"},
		{Kind: catalog.KindProject, Name: "project"},
		{Kind: catalog.KindKubernetes, Name: "cluster"},
		{Kind: catalog.KindDocker, Name: "docker"},
		{Kind: catalog.KindWorkspace, Name: "workspace"},
	} {
		picker := catalogActionPicker(cfg, encodeCatalogRef(ref))
		foundHide, foundDelete := false, false
		for _, option := range picker.Options {
			foundHide = foundHide || option.Name == "hide-entity" && option.Label == "Hide"
			foundDelete = foundDelete || option.Name == "remove-entity" && option.Label == "Delete from ContextHop" && strings.Contains(option.Detail, "never delete the external resource")
		}
		if foundHide != (ref.Kind != catalog.KindWorkspace) || !foundDelete {
			t.Errorf("%s actions do not expose hide/delete semantics: %#v", ref.Kind, picker.Options)
		}
	}
}

func TestCatalogForgetPreviewsRediscoverySemantics(t *testing.T) {
	cfg := config.New()
	cfg.Identities["unused"] = config.Identity{Provider: "gcp", Account: "unused@example.com"}
	editor := &catalogEditorState{}
	entity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "unused"})
	transition := catalogActionTransition(cfg, "forget-entity", ui.Draft{screenCatalogList: entity}, editor)
	if transition.Picker.Screen != ui.ScreenConfirm || !transition.Picker.HideSearch || editor.plan == nil || !editor.plan.Valid() || !pickerHasOption(transition.Picker, "apply", "Forget identity") {
		t.Fatalf("forget transition = %#v, plan = %#v", transition, editor.plan)
	}
	if !strings.Contains(transition.Picker.Description, "forget identity:unused") || !strings.Contains(transition.Picker.Description, "discovery may add it again") {
		t.Fatalf("forget preview = %q", transition.Picker.Description)
	}
}

func TestCatalogIdentityAndProjectDrillDown(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["payments"] = config.Project{Provider: "gcp", ProjectID: "payments", Identities: []string{"work"}}
	cfg.Kubernetes["api"] = config.Kubernetes{Type: "gke", Project: "payments", Cluster: "api", Location: "us-central1"}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	category := catalogCategoryPrefix + "identities"
	identities := flow(ui.Choice{Screen: ui.ScreenCatalogEntity, Option: ui.Option{Name: category}}, ui.Draft{ui.ScreenCatalogEntity: category})
	if identities.Picker.Screen != screenCatalogList || len(identities.Picker.Options) != 1 || identities.Picker.Options[0].Label != "work@example.com" {
		t.Fatalf("identity category = %#v", identities.Picker)
	}
	identity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "work"})
	projects := flow(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: identity}}, ui.Draft{ui.ScreenCatalogEntity: category, screenCatalogList: identity})
	if projects.Picker.Screen != screenCatalogIdentity || !pickerHasOption(projects.Picker, encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "payments"}), "Project: payments") {
		t.Fatalf("identity drill-down = %#v", projects.Picker)
	}

	project := encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "payments"})
	targets := flow(ui.Choice{Screen: screenCatalogIdentity, Option: ui.Option{Name: project}}, ui.Draft{screenCatalogIdentity: project})
	if targets.Picker.Screen != screenCatalogProject || !pickerHasOption(targets.Picker, "entity-map", "Map") || !pickerHasOption(targets.Picker, encodeCatalogRef(catalog.Ref{Kind: catalog.KindKubernetes, Name: "api"}), "Kubernetes: api") {
		t.Fatalf("project drill-down = %#v", targets.Picker)
	}

	target := encodeCatalogRef(catalog.Ref{Kind: catalog.KindKubernetes, Name: "api"})
	actions := flow(ui.Choice{Screen: screenCatalogProject, Option: ui.Option{Name: target}}, ui.Draft{screenCatalogProject: target})
	if actions.Picker.Screen != ui.ScreenCatalogAction || actions.Picker.Title != "Catalog › Kubernetes › api" {
		t.Fatalf("target actions = %#v", actions.Picker)
	}
}

func TestIdentityCatalogKeepsDetailsAndActionsOnList(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: "2026-09-04T18:55:43Z"}
	cfg.Identities["unused"] = config.Identity{Provider: "gcp", Account: "unused@example.com", Provenance: "manual"}
	cfg.Projects["payments"] = config.Project{Provider: "gcp", ProjectID: "payments-prod", Identities: []string{"work"}}

	picker := catalogCategoryPicker(cfg, "identities")
	if !picker.DisableEnter || !picker.ShowSelectedInfo || !picker.ModalActions || picker.Screen != screenCatalogList {
		t.Fatalf("identity catalog mode = %#v", picker)
	}
	var work, unused ui.Option
	for _, option := range picker.Options {
		switch option.Name {
		case encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "work"}):
			work = option
		case encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "unused"}):
			unused = option
		}
	}
	if !strings.Contains(work.Summary, "source:GCP verified:GCP observed:2026-09-04T18:55:43Z") || !strings.Contains(work.Summary, "Mappings: project:payments-prod") {
		t.Fatalf("selected identity details = %q", work.Summary)
	}
	for _, expected := range []struct{ key, action string }{{"h", "hide-entity"}, {"ctrl+d", "remove-entity"}} {
		found := false
		for _, action := range work.Actions {
			found = found || action.Key == expected.key && action.Action == expected.action
		}
		if !found {
			t.Errorf("work identity missing action %#v: %#v", expected, work.Actions)
		}
	}
	for _, action := range unused.Actions {
		if action.Action == "identity-unmap-project" {
			t.Fatalf("unmapped identity exposes unmap: %#v", unused.Actions)
		}
	}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	entity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "work"})
	draft := ui.Draft{screenCatalogList: entity}
	mapTarget := flow(ui.Choice{Screen: screenCatalogList, Option: work, Action: "identity-map-project"}, draft)
	if mapTarget.Picker.Screen != screenCatalogIdentityMap || !pickerHasOption(mapTarget.Picker, "payments", "payments-prod") {
		t.Fatalf("direct map transition = %#v", mapTarget.Picker)
	}
	unmapTarget := flow(ui.Choice{Screen: screenCatalogList, Option: work, Action: "identity-unmap-project"}, draft)
	if unmapTarget.Picker.Screen != screenCatalogIdentityUnmap || !pickerHasOption(unmapTarget.Picker, "payments", "payments-prod") {
		t.Fatalf("direct unmap transition = %#v", unmapTarget.Picker)
	}

	unusedEntity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "unused"})
	editor := &catalogEditorState{}
	forget := interactiveFlow(cfg, cfg, editor)(ui.Choice{Screen: screenCatalogList, Option: unused, Action: "forget-entity"}, ui.Draft{screenCatalogList: unusedEntity})
	if forget.Picker.Screen != ui.ScreenConfirm || editor.plan == nil || !editor.plan.Valid() {
		t.Fatalf("direct forget transition = %#v, plan %#v", forget.Picker, editor.plan)
	}
}

func TestEveryCatalogCategoryUsesInlineRowActions(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"work"}}
	cfg.Kubernetes["gke"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "local"}
	cfg.Docker["docker"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["workspace"] = config.Destination{Docker: "docker"}

	tests := []struct {
		category string
		ref      catalog.Ref
		actions  []string
	}{
		{"projects", catalog.Ref{Kind: catalog.KindProject, Name: "project"}, []string{"entity-map", "entity-labels", "hide-entity", "remove-entity"}},
		{"kubernetes", catalog.Ref{Kind: catalog.KindKubernetes, Name: "gke"}, []string{"entity-map", "entity-labels", "hide-entity", "remove-entity"}},
		{"kubernetes", catalog.Ref{Kind: catalog.KindKubernetes, Name: "local"}, []string{"entity-map", "entity-labels", "hide-entity", "remove-entity"}},
		{"docker", catalog.Ref{Kind: catalog.KindDocker, Name: "docker"}, []string{"docker-map-workspace", "docker-unmap-workspace", "hide-entity", "remove-entity"}},
		{"workspaces", catalog.Ref{Kind: catalog.KindWorkspace, Name: "workspace"}, []string{"workspace-configure", "remove-entity"}},
	}
	for _, test := range tests {
		picker := catalogCategoryPicker(cfg, test.category)
		if !picker.DisableEnter || !picker.ShowSelectedInfo || !picker.ModalActions {
			t.Errorf("%v does not use inline row mode: %#v", test.ref, picker)
			continue
		}
		encoded := encodeCatalogRef(test.ref)
		var selected ui.Option
		for _, option := range picker.Options {
			if option.Name == encoded {
				selected = option
				break
			}
		}
		if selected.Name == "" || selected.Summary == "" {
			t.Errorf("%v missing inline metadata: %#v", test.ref, selected)
			continue
		}
		for _, expected := range test.actions {
			found := false
			for _, action := range selected.Actions {
				found = found || action.Action == expected
			}
			if !found {
				t.Errorf("%v missing inline action %q: %#v", test.ref, expected, selected.Actions)
			}
		}
	}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	transitionTests := []struct {
		ref    catalog.Ref
		action string
		screen ui.Screen
	}{
		{catalog.Ref{Kind: catalog.KindProject, Name: "project"}, "project-map-identity", screenCatalogProjectMap},
		{catalog.Ref{Kind: catalog.KindProject, Name: "project"}, "project-unmap-identity", screenCatalogProjectUnmap},
		{catalog.Ref{Kind: catalog.KindKubernetes, Name: "gke"}, "kubernetes-map-identity", screenCatalogKubernetesIdentity},
		{catalog.Ref{Kind: catalog.KindKubernetes, Name: "local"}, "kubernetes-set-project", screenCatalogKubernetesProject},
		{catalog.Ref{Kind: catalog.KindDocker, Name: "docker"}, "docker-map-workspace", screenCatalogDockerMap},
		{catalog.Ref{Kind: catalog.KindDocker, Name: "docker"}, "docker-unmap-workspace", screenCatalogDockerUnmap},
		{catalog.Ref{Kind: catalog.KindWorkspace, Name: "workspace"}, "workspace-configure", screenAddWorkspaceBuilder},
	}
	for _, test := range transitionTests {
		encoded := encodeCatalogRef(test.ref)
		transition := flow(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: encoded}, Action: test.action}, ui.Draft{screenCatalogList: encoded})
		if transition.Picker.Screen != test.screen {
			t.Errorf("%v %s opened %q, want %q: %#v", test.ref, test.action, transition.Picker.Screen, test.screen, transition.Picker)
		}
	}
}

func TestResourceBrowserTitlesExposeScopeAndRealRecordCount(t *testing.T) {
	cfg := config.Config{
		Identities: map[string]config.Identity{"work": {Provider: "gcp", Account: "person@example.com"}},
		Projects: map[string]config.Project{"payments": {
			Provider: "gcp", ProjectID: "payments-prod", Identities: []string{"work"},
		}},
		Kubernetes: map[string]config.Kubernetes{"api": {
			Type: "gke", Project: "payments", Cluster: "api", Location: "us-central1",
		}},
	}
	projects := resourceBrowserPicker(cfg, ui.ScreenProject, sessionProjectOptions(cfg, "work"), "person@example.com")
	if projects.Title != "1 project" || projects.ScopeLabel != "Projects for person@example.com" || !projects.ResourceBrowser || !projects.Scoped {
		t.Fatalf("project browser = %#v", projects)
	}
	if len(projects.Options) != 1 {
		t.Fatalf("project table should contain only resources: %#v", projects.Options)
	}

	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	transition := flow(ui.Choice{
		Screen: ui.ScreenProject,
		Option: ui.Option{Name: "payments", Project: true, ProjectID: "payments-prod"},
		Action: "browse-kubernetes",
	}, ui.Draft{ui.ScreenIdentity: "work", ui.ScreenProject: "payments"})
	if !transition.ReplaceCurrent || transition.Picker.Title != "1 Kubernetes target" || transition.Picker.ScopeLabel != "Kubernetes for person@example.com → payments-prod" {
		t.Fatalf("Kubernetes scope transition = %#v", transition)
	}
}

func TestResourceBrowserActionsAvoidNavigationKeyCollisions(t *testing.T) {
	cfg := config.Config{
		Projects:   map[string]config.Project{"project": {ProjectID: "example"}},
		Kubernetes: map[string]config.Kubernetes{"cluster": {Type: "kubeconfig", Project: "project"}},
	}
	picker := resourceBrowserPicker(cfg, ui.ScreenKubernetes, kubernetesOptions(cfg, ""), "")
	if len(picker.Options) != 1 {
		t.Fatalf("Kubernetes options = %#v", picker.Options)
	}
	keys := map[string]string{}
	for _, action := range picker.Options[0].Actions {
		keys[action.Action] = action.Key
	}
	for action, want := range map[string]string{
		"entity-map": "m", "entity-labels": "t", "hide-entity": "H", "remove-entity": "ctrl+d",
	} {
		if keys[action] != want {
			t.Errorf("action %q key = %q, want %q", action, keys[action], want)
		}
	}
}

func TestStartupPrefersSavedWorkspaces(t *testing.T) {
	history := recency.History{LastUsed: map[string]map[string]time.Time{"kubernetes": {"api": time.Now()}}}
	cfg := config.New()
	if got := recentResourceScreen(history, cfg); got != ui.ScreenIdentity {
		t.Fatalf("empty catalog starts on %s", got)
	}
	cfg.Destinations["dev"] = config.Destination{}
	if got := recentResourceScreen(history, cfg); got != ui.ScreenWorkspace {
		t.Fatalf("saved workspace starts on %s", got)
	}
	cfg.Destinations["dev"] = config.Destination{Hidden: true}
	if got := recentResourceScreen(history, cfg); got != ui.ScreenWorkspace {
		t.Fatalf("hidden-only workspaces start on %s", got)
	}
}

func TestHiddenCatalogSuppressesResourcesAndSupportsUnhideOrDelete(t *testing.T) {
	cfg := config.New()
	cfg.Identities["hidden-identity"] = config.Identity{Provider: "gcp", Account: "hidden@example.com", Hidden: true}
	cfg.Projects["hidden-project"] = config.Project{Provider: "gcp", ProjectID: "hidden-project", Hidden: true}
	cfg.Kubernetes["hidden-cluster"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "hidden", Hidden: true}
	cfg.Docker["hidden-docker"] = config.Docker{Context: "hidden", Hidden: true}
	cfg.Destinations["hidden-workspace"] = config.Destination{Docker: "hidden-docker", Hidden: true}

	root := catalogEntityOptions(cfg)
	if !pickerHasOption(ui.Picker{Options: root}, catalogCategoryPrefix+"hidden", "Hidden") {
		t.Fatalf("catalog root has no Hidden category: %#v", root)
	}
	hidden := catalogCategoryPicker(cfg, "hidden")
	if len(hidden.Options) != 4 || !hidden.DisableEnter || !hidden.ShowSelectedInfo || !hidden.ModalActions {
		t.Fatalf("hidden catalog = %#v", hidden)
	}
	for _, option := range hidden.Options {
		if !strings.Contains(option.Summary, "visibility:HIDDEN") {
			t.Errorf("hidden option lacks visibility metadata: %#v", option)
		}
		for _, expected := range []string{"unhide-entity", "remove-entity"} {
			found := false
			for _, action := range option.Actions {
				found = found || action.Action == expected
			}
			if !found {
				t.Errorf("hidden option lacks %q: %#v", expected, option.Actions)
			}
		}
	}
	if len(catalogCategoryPicker(cfg, "identities").Options) != 0 || len(catalogCategoryPicker(cfg, "projects").Options) != 0 || len(catalogCategoryPicker(cfg, "kubernetes").Options) != 0 || len(catalogCategoryPicker(cfg, "docker").Options) != 0 || len(catalogCategoryPicker(cfg, "workspaces").Options) != 1 {
		t.Fatal("a hidden resource remained in a normal catalog category")
	}

	entity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "hidden-identity"})
	option := hidden.Options[0]
	for _, candidate := range hidden.Options {
		if candidate.Name == entity {
			option = candidate
		}
	}
	editor := &catalogEditorState{}
	transition := interactiveFlow(cfg, cfg, editor)(ui.Choice{Screen: screenCatalogList, Option: option, Action: "unhide-entity"}, ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "hidden", screenCatalogList: entity})
	if !transition.Complete || transition.Picker.Screen != "" || editor.plan == nil || !editor.plan.Valid() || editor.plan.Config.Identities["hidden-identity"].Hidden || !catalogVisibilityPlan(editor.plan) {
		t.Fatalf("unhide transition = %#v, plan %#v", transition, editor.plan)
	}

	deleteEditor := &catalogEditorState{}
	deleted := interactiveFlow(cfg, cfg, deleteEditor)(ui.Choice{Screen: screenCatalogList, Option: option, Action: "remove-entity"}, ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "hidden", screenCatalogList: entity})
	if deleted.Picker.Screen != ui.ScreenConfirm || deleteEditor.plan == nil || !deleteEditor.plan.Valid() || !pickerHasOption(deleted.Picker, "apply", "Delete from ContextHop") || !strings.Contains(deleted.Picker.Description, "external resource is not changed") {
		t.Fatalf("delete transition = %#v, plan %#v", deleted.Picker, deleteEditor.plan)
	}
}

func TestHideAppliesWithoutConfirmationButDeleteStillConfirms(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	entity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "person"})
	draft := ui.Draft{ui.ScreenCatalogEntity: catalogCategoryPrefix + "identities", screenCatalogList: entity}

	hideEditor := &catalogEditorState{}
	hide := interactiveFlow(cfg, cfg, hideEditor)(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: entity}, Action: "hide-entity"}, draft)
	if !hide.Complete || hide.Picker.Screen != "" || hideEditor.plan == nil || !hideEditor.plan.Config.Identities["person"].Hidden || !catalogVisibilityPlan(hideEditor.plan) {
		t.Fatalf("hide should apply immediately: transition %#v, plan %#v", hide, hideEditor.plan)
	}

	deleteEditor := &catalogEditorState{}
	deleted := interactiveFlow(cfg, cfg, deleteEditor)(ui.Choice{Screen: screenCatalogList, Option: ui.Option{Name: entity}, Action: "remove-entity"}, draft)
	if deleted.Complete || deleted.Picker.Screen != ui.ScreenConfirm || deleteEditor.plan == nil || catalogVisibilityPlan(deleteEditor.plan) {
		t.Fatalf("delete should retain confirmation: transition %#v, plan %#v", deleted, deleteEditor.plan)
	}
}

func TestWorkspaceHideAppliesWithoutConfirmation(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["local"] = config.Destination{Docker: "local"}
	editor := &catalogEditorState{workspace: &workspaceEditorDraft{Name: "local", Value: cfg.Destinations["local"], Existing: true}}
	transition := catalogAddStep(cfg, ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-hide"}}, nil, editor)
	if !transition.Complete || transition.Picker.Screen != "" || editor.plan == nil || !editor.plan.Config.Destinations["local"].Hidden || !catalogVisibilityPlan(editor.plan) {
		t.Fatalf("workspace hide = transition %#v, plan %#v", transition, editor.plan)
	}
}

func TestHiddenResourcesAreExcludedFromNewChoicesButExistingMappingsResolve(t *testing.T) {
	cfg := config.New()
	cfg.Identities["hidden"] = config.Identity{Provider: "gcp", Account: "hidden@example.com", Hidden: true}
	cfg.Projects["hidden"] = config.Project{Provider: "gcp", ProjectID: "hidden", Identities: []string{"hidden"}, Hidden: true}
	cfg.Kubernetes["hidden"] = config.Kubernetes{Type: "gke", Project: "hidden", Cluster: "hidden", Location: "us-central1", Hidden: true}
	cfg.Docker["hidden"] = config.Docker{Context: "hidden", Hidden: true}
	cfg.Destinations["hidden"] = config.Destination{Identity: "hidden", Project: "hidden", Kubernetes: "hidden", Docker: "hidden", Hidden: true}

	for label, options := range map[string][]ui.Option{
		"identity": identityOptions(cfg, nil), "compatible identity": compatibleIdentityOptions(cfg, "gcp"),
		"project": projectOptions(cfg, ""), "kubernetes": kubernetesOptions(cfg, ""), "docker": dockerOptions(cfg), "workspace": destinationOptions(cfg, "workspace"),
	} {
		if len(options) != 0 {
			t.Errorf("hidden %s remained selectable: %#v", label, options)
		}
	}
	// An explicit relationship is still usable, so hiding cannot silently
	// invalidate a configured project or workspace.
	if options := identityOptions(cfg, cfg.Projects["hidden"].Identities); len(options) != 1 || options[0].Name != "hidden" {
		t.Fatalf("existing hidden mapping no longer resolves: %#v", options)
	}
	if _, err := resolver.Destination(cfg, "hidden"); err != nil {
		t.Fatalf("hidden workspace no longer resolves: %v", err)
	}
	visibleWorkspace := cfg.Destinations["hidden"]
	visibleWorkspace.Hidden = false
	cfg.Destinations["visible"] = visibleWorkspace
	draft := &workspaceEditorDraft{Name: "visible", Value: visibleWorkspace, Existing: true, Editing: catalog.KindIdentity}
	picker := workspaceComponentPicker(cfg, draft, screenAddWorkspaceComponent)
	if !pickerContainsDetail(picker, "HIDDEN") || !pickerHasOption(picker, "hidden", "hidden@example.com") {
		t.Fatalf("existing hidden component was not identified in workspace editor: %#v", picker.Options)
	}
}

func TestCatalogEntityDetailsHaveNoManageOrSelfReferentialLayer(t *testing.T) {
	cfg := config.New()
	cfg.Identities["identity"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project", Identities: []string{"identity"}}
	cfg.Kubernetes["kubernetes"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}
	cfg.Docker["docker"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["workspace"] = config.Destination{Identity: "identity", Project: "project", Kubernetes: "kubernetes", Docker: "docker"}

	for _, ref := range []catalog.Ref{
		{Kind: catalog.KindIdentity, Name: "identity"},
		{Kind: catalog.KindProject, Name: "project"},
		{Kind: catalog.KindKubernetes, Name: "kubernetes"},
		{Kind: catalog.KindDocker, Name: "docker"},
		{Kind: catalog.KindWorkspace, Name: "workspace"},
	} {
		var picker ui.Picker
		switch ref.Kind {
		case catalog.KindIdentity:
			picker = catalogIdentityPicker(cfg, ref)
		case catalog.KindProject:
			picker = catalogProjectPicker(cfg, ref)
		default:
			picker = catalogActionPicker(cfg, encodeCatalogRef(ref))
		}
		for _, option := range picker.Options {
			if strings.HasPrefix(option.Label, "Manage ") {
				t.Errorf("%s detail retains redundant action %q", ref.Kind, option.Label)
			}
			if option.Name == encodeCatalogRef(ref) {
				t.Errorf("%s detail links back to itself", ref.Kind)
			}
		}
	}

	editor := &catalogEditorState{}
	identity := encodeCatalogRef(catalog.Ref{Kind: catalog.KindIdentity, Name: "identity"})
	transition := interactiveFlow(cfg, cfg, editor)(
		ui.Choice{Screen: screenCatalogIdentity, Option: ui.Option{Name: "forget-entity"}},
		ui.Draft{screenCatalogList: identity, screenCatalogIdentity: "forget-entity"},
	)
	if transition.Picker.Screen != ui.ScreenConfirm || transition.Picker.Title != "Catalog › Change blocked" || len(transition.Picker.Options) != 0 || editor.plan == nil {
		t.Fatalf("inline identity action added another menu layer: %#v", transition)
	}
}

func pickerHasOption(picker ui.Picker, name, label string) bool {
	for _, option := range picker.Options {
		if option.Name == name && option.Label == label {
			return true
		}
	}
	return false
}

func TestValidateSelectionFreshOnlyRejectsSelectedChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	cfg := config.New()
	cfg.Identities["selected"] = config.Identity{Provider: "gcp", Account: "selected@example.com"}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	baseline := cfg.Clone()
	selection := resolver.Selection{Identity: "selected"}

	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "changed@example.com"}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateSelectionFresh(baseline, selection); err != nil {
		t.Fatalf("unrelated change rejected: %v", err)
	}

	cfg.Identities["selected"] = config.Identity{Provider: "gcp", Account: "new@example.com"}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateSelectionFresh(baseline, selection); err == nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("selected change error = %v", err)
	}
}

func TestCurrentContextEquivalentForDockerOnlySession(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	resolved := resolver.Resolved{
		Name: "local", DockerName: "local", Docker: &config.Docker{Context: "orbstack"},
	}
	prepared, err := session.Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, item := range prepared.Env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			t.Setenv(key, value)
		}
	}

	equivalent, err := currentContextEquivalent(resolved)
	if err != nil || !equivalent {
		t.Fatalf("equivalent = %v, err = %v", equivalent, err)
	}
	resolved.Risk = "production"
	equivalent, err = currentContextEquivalent(resolved)
	if err != nil || !equivalent {
		t.Fatalf("metadata-only context equivalent = %v, err = %v", equivalent, err)
	}
	resolved.PromptColors = map[string]string{"docker": "#123456"}
	equivalent, err = currentContextEquivalent(resolved)
	if err != nil || equivalent {
		t.Fatalf("changed prompt colour must refresh: equivalent=%v, err=%v", equivalent, err)
	}
}

func TestSwitchDoesNotContactDockerBeforeActivation(t *testing.T) {
	cache := t.TempDir()
	bin := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	t.Setenv("CONTEXTHOP_CONTEXT", "previous")
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf 'connection refused' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.Resolved{Name: "next", DockerName: "desktop", Docker: &config.Docker{Context: "desktop-linux"}}
	err := runResolved(resolved, []string{"/usr/bin/true"})
	if err != nil {
		t.Fatalf("switch contacted Docker before activation: %v", err)
	}
	if os.Getenv("CONTEXTHOP_CONTEXT") != "previous" {
		t.Fatalf("context changed to %q", os.Getenv("CONTEXTHOP_CONTEXT"))
	}
	sessionRoot := filepath.Join(cache, "contexthop", "sessions")
	entries, readErr := os.ReadDir(sessionRoot)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staged sessions remain: %v", entries)
	}
}

func TestOldKubernetesSessionWithoutFingerprintIsNotEquivalent(t *testing.T) {
	directory := t.TempDir()
	kubeconfig := filepath.Join(directory, "kubeconfig")
	contents := `apiVersion: v1
kind: Config
current-context: work
contexts:
- name: work
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: https://api.example
    insecure-skip-tls-verify: true
`
	if err := os.WriteFile(kubeconfig, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	target := &config.Kubernetes{Type: "kubeconfig", Kubeconfig: kubeconfig, Context: "work"}
	revision, err := discovery.KubernetesSourceRevision(*target)
	if err != nil {
		t.Fatal(err)
	}
	manifest := state.Manifest{
		Version: 1, SessionID: "old-session", Destination: "work",
		Expected:                 state.Component{Kubernetes: "work", Namespace: "default", Docker: "contexthop-none", ADC: filepath.Join(directory, "disabled-google-credentials.json")},
		KubernetesTargetIdentity: resolver.KubernetesTargetIdentity(*target, nil), KubernetesSourceRevision: revision,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "session.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTHOP_CONTEXT", "work")
	t.Setenv(state.SessionFileEnv, manifestPath)
	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("DOCKER_CONTEXT", "contexthop-none")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", manifest.Expected.ADC)
	t.Setenv("AWS_PROFILE", "contexthop-none")
	equivalent, err := currentContextEquivalent(resolver.Resolved{Name: "work", KubernetesName: "work", Kubernetes: target})
	if err != nil {
		t.Fatal(err)
	}
	if equivalent {
		t.Fatal("old Kubernetes session without a control-plane fingerprint was treated as equivalent")
	}
}

func TestProjectSearchRecoveryUsesActionDialog(t *testing.T) {
	action := ui.Option{Name: authenticateProjectSearchPrefix + "work", Project: true, ProjectID: "Authenticate and retry…"}
	picker := projectSearchRecoveryPicker("work@example.com", action, true, "provider error")
	if !picker.HideSearch || picker.Dimension == "project" || picker.Options[0].Name != action.Name || picker.Options[0].Label != "Authenticate and retry" {
		t.Fatalf("recovery should preserve the action without a resource table: %#v", picker)
	}
	if !strings.Contains(picker.Description, "work@example.com") || picker.EnterLabel == "" {
		t.Fatalf("missing recovery context: %#v", picker)
	}
}

func TestDiscoveredProjectContinuesInMainBrowser(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	catalogCfg := config.New()
	pickers := interactivePickers(cfg, catalogCfg, nil)
	// Discovery adds session-only projects after the initial pickers are built.
	cfg.Projects["local-key"] = config.Project{Provider: "gcp", ProjectID: "discovered-project", Identities: []string{"work"}}
	flow := interactiveFlow(cfg, catalogCfg, &catalogEditorState{})
	model := ui.NewAppModel(ui.AppOptions{
		StartScreen: ui.ScreenProject, ResourceBrowser: true, InitialDraft: ui.Draft{ui.ScreenIdentity: "work"}, Pickers: pickers,
		BrowserPicker: func(screen ui.Screen, draft ui.Draft) ui.Picker {
			return interactiveBrowserPicker(cfg, catalogCfg, screen, draft)
		},
		Flow: func(choice ui.Choice, draft ui.Draft) ui.Transition {
			if choice.Screen == ui.ScreenProject {
				return ui.Transition{Picker: ui.Picker{Screen: screenProjectSearchResults, Dimension: "project", Options: []ui.Option{{Name: "local-key", Project: true, ProjectID: "discovered-project"}}}}
			}
			return flow(choice, draft)
		},
	})
	for range 2 {
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if command != nil {
			t.Fatal("project selection should not launch a shell")
		}
		model = updated.(ui.AppModel)
	}
	if model.CurrentScreen() != ui.ScreenKubernetes || model.StackDepth() != 1 {
		t.Fatalf("search result left a nested picker: %s depth %d", model.CurrentScreen(), model.StackDepth())
	}
	view := ansi.Strip(model.View().Content)
	for _, expected := range []string{"discovered-project", "work@example.com", "Kubernetes", "No Kubernetes"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("browser missing %q: %s", expected, view)
		}
	}
	if strings.Contains(view, "type to filter") {
		t.Fatal("main browser retained dialog search box")
	}
	for range 3 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		model = updated.(ui.AppModel)
	}
	view = ansi.Strip(model.View().Content)
	if strings.Contains(view, "Project: discovered-project") || model.StackDepth() != 1 {
		t.Fatal("Escape restored the discovery dialog or its selection")
	}

	transition := flow(ui.Choice{Screen: screenProjectSearchResults, Option: ui.Option{Name: "local-key"}}, ui.Draft{screenProjectSearchIdentity: "work"})
	draft := ui.Draft{screenProjectSearchResults: "local-key", screenProjectSearchIdentity: "work"}
	for screen, value := range transition.DraftUpdates {
		draft[screen] = value
	}
	if draft[ui.ScreenIdentity] != "work" || draft[ui.ScreenProject] != "local-key" || draft[screenProjectSearchResults] != "" || draft[screenProjectSearchIdentity] != "" {
		t.Fatalf("discovery draft not committed: %#v", draft)
	}
	draft[ui.ScreenProject] = ""
	selection, err := selectionFromDraft(cfg, draft)
	if err != nil || selection.Project != "" {
		t.Fatalf("cleared project restored from stale search state: %#v, %v", selection, err)
	}
}

func TestDiscoveryBrowserSeparatesSavedProjectsAndSavesOnlyChosenResult(t *testing.T) {
	saved := config.New()
	saved.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	saved.Projects["existing"] = config.Project{Provider: "gcp", ProjectID: "existing", Identities: []string{"work"}}
	selection := saved.Clone()
	for _, name := range []string{"new-one", "new-two"} {
		selection.Projects[name] = config.Project{Provider: "gcp", ProjectID: name, Identities: []string{"work"}, Provenance: "gcp"}
	}
	transition := discoveredProjectsTransition(selection, saved, "work", []ui.Option{{Name: "new-one"}, {Name: "new-two"}}, nil)
	if !transition.ReplaceCurrent || transition.Picker.Screen != ui.ScreenProject || !transition.Picker.ResourceBrowser {
		t.Fatalf("discovery did not return to project browser: %#v", transition)
	}
	if transition.Picker.Title != "3 projects" || transition.DraftUpdates[ui.ScreenProject] != "" {
		t.Fatalf("incorrect count or automatic selection: %#v", transition)
	}
	var discovered ui.Option
	for _, option := range transition.Picker.Options {
		switch option.Name {
		case "existing":
			if option.SaveStatus != "Saved" {
				t.Fatalf("saved status missing: %#v", option)
			}
		case "new-one":
			discovered = option
			if option.SaveStatus != "Unsaved" || len(option.Actions) != 1 || option.Actions[0].Action != "save-project" {
				t.Fatalf("save affordance missing: %#v", option)
			}
		}
	}
	editor := &catalogEditorState{}
	flow := interactiveFlow(selection, saved, editor)
	confirmation := flow(ui.Choice{Screen: ui.ScreenProject, Option: discovered, Action: "save-project"}, ui.Draft{ui.ScreenIdentity: "work"})
	if !confirmation.Complete || !editor.applyImmediately || editor.plan == nil || !editor.plan.Valid() {
		t.Fatalf("save did not prepare immediate apply: %#v", confirmation)
	}
	if len(saved.Projects) != 1 {
		t.Fatal("preparing save mutated catalog")
	}
	if len(editor.plan.Config.Projects) != 2 || editor.plan.Config.Projects["new-one"].ProjectID != "new-one" {
		t.Fatal("save included unchosen results")
	}
	if !slices.Contains(editor.plan.Config.Projects["new-one"].Identities, "work") {
		t.Fatal("save lost discovered identity mapping")
	}
	refreshed := interactiveBrowserPicker(selection, editor.plan.Config, ui.ScreenProject, ui.Draft{ui.ScreenIdentity: "work"})
	for _, option := range refreshed.Options {
		if option.Name == "new-one" && option.SaveStatus != "Saved" {
			t.Fatal("saved project still marked unsaved")
		}
		if option.Name == "new-two" && option.SaveStatus != "Unsaved" {
			t.Fatal("other discovery lost unsaved status")
		}
	}
}

func TestResourceAddStartsWithCurrentResourceType(t *testing.T) {
	cfg := config.New()
	for screen, want := range map[ui.Screen]ui.Screen{ui.ScreenIdentity: screenAddIdentityName, ui.ScreenProject: ui.ScreenDependencyTarget, ui.ScreenKubernetes: ui.ScreenDependencyTarget, ui.ScreenDocker: screenAddDockerContext, ui.ScreenWorkspace: screenAddWorkspaceKind} {
		transition := interactiveFlow(cfg, cfg, &catalogEditorState{})(ui.Choice{Screen: screen, Action: "add-resource"}, ui.Draft{})
		if transition.Picker.Screen != want || (screen != ui.ScreenWorkspace && transition.DraftUpdates[ui.ScreenCatalogAction] != "add-"+string(screen)) {
			t.Fatalf("%s add did not skip type chooser: %#v", screen, transition)
		}
	}
}

func TestDiscoverSaveAndMapPersistAfterBrowserRestart(t *testing.T) {
	for _, scenario := range []struct{ name, project, key, status string }{
		{"save new project", "new-project", "S", "Unsaved"},
		{"save current identity mapping", "existing-project", "S", "Unmapped"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			t.Setenv("CONTEXTHOP_CONFIG", path)
			t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte("#!/bin/sh\n[ \"$1 $2\" = 'auth print-access-token' ] && exit 0\n[ \"$1 $2\" = 'projects list' ] || exit 1\n[ \"$CLOUDSDK_CORE_ACCOUNT\" = 'work@example.com' ] || exit 2\nprintf '%s' '[{\"projectId\":\"new-project\"},{\"projectId\":\"existing-project\"},{\"projectId\":\"leave-unsaved\"}]'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			cfg := config.New()
			cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: filepath.Join(dir, "gcloud-work")}
			cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
			cfg.Projects["existing-project"] = config.Project{Provider: "gcp", ProjectID: "existing-project", Identities: []string{"other"}}
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			selection := cfg.Clone()
			editor := &catalogEditorState{}
			model := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenIdentity, ResourceBrowser: true,
				Pickers: interactivePickers(selection, cfg, nil), Flow: interactiveFlow(selection, cfg, editor),
				BrowserPicker: func(screen ui.Screen, draft ui.Draft) ui.Picker {
					return interactiveBrowserPicker(selection, cfg, screen, draft)
				},
			})
			press := func(key string) {
				msg := tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
				if key == "enter" {
					msg = tea.KeyPressMsg{Code: tea.KeyEnter}
				}
				if key == "esc" {
					msg = tea.KeyPressMsg{Code: tea.KeyEscape}
				}
				updated, _ := model.Update(msg)
				model = updated.(ui.AppModel)
			}
			search := func(text string) {
				press("/")
				for _, r := range text {
					press(string(r))
				}
			}
			search("work@example.com")
			press("enter")
			press("d")
			press("enter")
			if model.CurrentScreen() != ui.ScreenProject || model.StackDepth() != 1 {
				t.Fatalf("discovery left main browser: %s", model.View().Content)
			}
			search(scenario.project)
			if !strings.Contains(ansi.Strip(model.View().Content), scenario.project) {
				t.Fatalf("missing project %s: %s", scenario.project, model.View().Content)
			}
			// Exit search without activating the project, then locate the row by
			// its stable sorted position (existing, leave-unsaved, new, actions).
			press("esc")
			if scenario.project == "new-project" {
				for range 2 {
					updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
					model = updated.(ui.AppModel)
				}
			}
			press(scenario.key)
			if model.CurrentScreen() == ui.ScreenConfirm || !editor.applyImmediately || editor.plan == nil {
				t.Fatalf("save should finish without another confirmation: %s", model.View().Content)
			}
			if err := catalog.Apply(path, *editor.plan); err != nil {
				t.Fatal(err)
			}
			fresh, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if mappedIdentityForProject(fresh, scenario.project, "work") != "work" {
				t.Fatal("saved mapping did not survive reload")
			}
			if _, exists := fresh.Projects["leave-unsaved"]; exists {
				t.Fatal("unselected project persisted")
			}
			if scenario.project == "existing-project" && !slices.Contains(fresh.Projects[scenario.project].Identities, "other") {
				t.Fatal("existing mapping was overwritten")
			}
			reopened := interactiveBrowserPicker(fresh.Clone(), fresh, ui.ScreenProject, ui.Draft{ui.ScreenIdentity: "work"})
			found := false
			for _, option := range reopened.Options {
				if option.Name == scenario.project {
					found = true
					if option.SaveStatus != "Saved" {
						t.Fatalf("reopened status = %s", option.SaveStatus)
					}
				}
			}
			if !found {
				t.Fatal("saved project absent after restart without discovery")
			}
		})
	}
}

func TestRoutineCatalogPolicyRetainsReviewForConsequentialChanges(t *testing.T) {
	for _, action := range []string{"remove", "forget", "unmap", "set"} {
		plan := catalog.Plan{Changes: []catalog.Change{{Action: action, To: catalog.Ref{Kind: catalog.KindProject, Name: "project"}}}}
		if routineCatalogPlan(plan) {
			t.Fatalf("%s bypassed review", action)
		}
	}
	mapping := catalog.Plan{Changes: []catalog.Change{{Action: "map", From: catalog.Ref{Kind: catalog.KindIdentity, Name: "work"}, To: catalog.Ref{Kind: catalog.KindProject, Name: "project"}}}}
	if !routineCatalogPlan(mapping) {
		t.Fatal("simple mapping still requires confirmation")
	}
	mapping.Impacts = []catalog.Impact{{Workspace: "production"}}
	if routineCatalogPlan(mapping) {
		t.Fatal("workspace behavior change bypassed review")
	}
	mapping.Impacts = nil
	mapping.Problems = []string{"invalid mapping"}
	if routineCatalogPlan(mapping) {
		t.Fatal("invalid mapping bypassed blocked state")
	}
}

// Configuration aliases must share the canonical route, even in a managed shell.
// A non-terminal invocation prints the path without loading a catalog or providers.
func TestConfigurationEntryPointsShareCanonicalRoute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	t.Setenv("CONTEXTHOP_CONTEXT", "current")
	t.Setenv(session.ActivationFileEnv, "")
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	commands := [][]string{
		{"config"}, {"catalog"}, {"c"}, {"m"}, {"map"},
		{"k"}, {"kubernetes"}, {"i"}, {"identity"}, {"p"}, {"project"}, {"d"}, {"docker"},
		{"config", "manage"}, {"catalog", "map"}, {"config", "browser"},
		{"select", "identity"}, {"select", "kubernetes"}, {"select", "project"}, {"select", "docker"},
		{"debug", "config"}, {"debug", "i"},
	}
	for _, args := range commands {
		if err := output.Truncate(0); err != nil {
			t.Fatal(err)
		}
		if _, err := output.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		if err := run(args); err != nil {
			t.Fatalf("run(%q): %v", args, err)
		}
		got, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != path+"\n" {
			t.Fatalf("run(%q) output = %q", args, got)
		}
	}
}
