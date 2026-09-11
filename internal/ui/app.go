package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/state"
)

// Screen identifies a page in the ContextHop application.
type Screen string

const (
	ScreenIdentity         Screen = "identity"
	ScreenProject          Screen = "project"
	ScreenKubernetes       Screen = "kubernetes"
	ScreenDocker           Screen = "docker"
	ScreenWorkspace        Screen = "workspace"
	ScreenWorkspaceSource  Screen = "workspace-source"
	ScreenReuse            Screen = "reuse"
	ScreenCatalogEntity    Screen = "catalog-entity"
	ScreenCatalogAction    Screen = "catalog-action"
	ScreenDependencyTarget Screen = "dependency-target"
	ScreenPreview          Screen = "preview"
	ScreenConfirm          Screen = "confirm"
	ScreenInput            Screen = "input"
)

// Choice is a typed selection from one picker screen.
type Choice struct {
	CanApplyShell  bool
	Context        context.Context
	ReportProgress func(string)
	Screen         Screen
	Option         Option
	Action         string
}

const ScreenShellADCOverride Screen = "shell-adc-override"

// Draft is the dialog transport format. ContextSelection extracts typed
// resource state; all other keys belong to navigation and workflow inputs.
type Draft map[Screen]string

// Outcome is returned only after a flow reaches a terminal selection.
type Outcome struct {
	Complete bool
	Choice   Choice
	Draft    Draft
}

// PickerField is a labeled value in a dialog’s context summary.
type PickerField struct {
	Label string
	Value string
}

// Picker describes a page of locally loaded options.
type Picker struct {
	optionsCommands  []KeyAction
	optionsTarget    Option
	previewDraft     Draft
	previewAction    string
	identityResource Option

	OperationActions  []KeyAction
	OperationDraft    Draft
	StatusKind        string
	ActionRows        bool
	ContextFields     []PickerField
	Flow              FlowFunc // Optional continuation owned by this dialog.
	EscapeAction      string
	KeepDraftOnCancel bool // Launch settings apply to the next launch after closing the dialog.
	CompactDialog     bool
	selectionTarget   Screen // Destination after choosing a resource identity.
	ReadOnlyText      string
	OperationDetails  string
	ShowHidden        bool
	ScopeLabel        string
	Screen            Screen
	Title             string
	Description       string
	Dimension         string
	Options           []Option
	Input             *Input
	HideSearch        bool
	DisableEnter      bool
	ShowSelectedInfo  bool
	ModalActions      bool
	ResourceBrowser   bool
	Scoped            bool
	EnterLabel        string
	Focus             string
}

// Input describes one generic text-entry field for guided catalog additions.
// Validate must be local and side-effect free.
type Input struct {
	AllowEmpty  bool
	Prompt      string
	Placeholder string
	Initial     string
	Validate    func(string) error
}

// Transition tells the application whether a choice finishes the flow, pushes
// another picker, or returns a refreshed picker to the previous stack frame.
type Transition struct {
	ReturnToScreen   Screen // Replace an existing workflow page, or the current page if absent.
	CompletionChoice *Choice
	Dismiss          bool
	PreservePosition bool
	PersistDiscovery bool
	// Result is an immutable application result, accepted on the event loop
	// before navigation or rendering. Workers must not publish mutable state.
	Result           any
	Pickers          map[Screen]Picker
	ResetNavigation  bool
	Complete         bool
	ReturnToPrevious bool
	ReplaceCurrent   bool
	DraftUpdates     Draft
	Picker           Picker
	Process          *Process
}

// Process temporarily yields the terminal to an interactive command, then
// resumes the same application with the transition returned by Done.
type Process struct {
	Continue    func(context.Context, func(string), error) Transition
	Cancellable bool
	Label       string
	Command     *exec.Cmd
	Done        func(error) Transition
}

type processFinishedMessage struct {
	process *Process
	err     error
}

// FlowFunc prepares the next workflow result. With AsyncFlow enabled it runs
// outside the event loop and must use private state snapshots. Result acceptance
// belongs to AcceptResult; terminal-interactive work must use Process.
type FlowFunc func(Choice, Draft) Transition

// BrowserPickerFunc rebuilds a resource tab for the current committed
// selection. It keeps tab switches and selection unwinds relationship-aware.
type BrowserPickerFunc func(Screen, Draft) Picker

// AppOptions configures the single Bubble Tea application.
type AppOptions struct {
	ReadSharedConfig    func() (*state.Manifest, error)
	Version             string
	LaunchPreview       func(Screen, Option, Draft) LaunchPreview
	InitialDialog       *Picker
	ComposeSelection    bool
	CanApplyShell       bool
	RefreshIdentityAuth func()
	AcceptResult        func(any)
	Snapshot            state.Snapshot
	StartScreen         Screen
	Pickers             map[Screen]Picker
	Flow                FlowFunc
	BrowserPicker       BrowserPickerFunc
	Probe               bool
	InitialDraft        Draft
	ResourceBrowser     bool
	AsyncFlow           bool // Run potentially slow workflow steps outside the event loop.
}

type navigationFrame struct {
	screen      Screen
	picker      Picker
	filter      string
	cursor      int
	cursorKey   string
	input       string
	inputErr    string
	textOffset  int
	inputOffset int // rune distance from the end of the input
	searching   bool
	draftBefore Draft
}

// AppModel owns the home page and every picker in one event loop.
type AppModel struct {
	readSharedConfig func() (*state.Manifest, error)
	version          string
	launchPreview    func(Screen, Option, Draft) LaunchPreview
	identityRecovery *identityRecovery

	browseAll           bool
	quitAfterWork       bool
	helpVisible         bool
	helpOffset          int
	composeSelection    bool
	canApplyShell       bool
	refreshIdentityAuth func()
	showHidden          map[Screen]bool
	tabPositions        map[Screen]tabPosition
	workspaceStage      uint64
	acceptResult        func(any)
	snapshot            state.Snapshot
	probing             bool
	probed              bool
	probeErr            []string
	width               int
	height              int

	stack                []navigationFrame
	pickers              map[Screen]Picker
	flow                 FlowFunc
	browser              BrowserPickerFunc
	draft                Draft
	outcome              Outcome
	asyncFlow            bool
	working              bool
	workID               int
	workStarted          time.Time
	workFrame            int
	workLabel            string
	resourceLabels       map[Screen]map[string]string
	initialResourceDraft Draft
	resourceContexts     map[string]string
	resourceNamespaces   map[string]string
	workCancel           context.CancelFunc
	workStopping         bool
	workCompact          bool
	workBackground       string
}

// NewAppModel builds a model suitable for a Bubble Tea program or unit tests.
func NewAppModel(options AppOptions) AppModel {
	model := AppModel{
		readSharedConfig: options.ReadSharedConfig,
		version:          options.Version,
		launchPreview:    options.LaunchPreview,
		acceptResult:     options.AcceptResult,
		composeSelection: options.ComposeSelection, canApplyShell: options.CanApplyShell,
		asyncFlow:           options.AsyncFlow,
		snapshot:            options.Snapshot,
		probing:             options.Probe,
		pickers:             clonePickers(options.Pickers),
		flow:                options.Flow,
		refreshIdentityAuth: options.RefreshIdentityAuth,
		browser:             options.BrowserPicker,
		draft:               cloneDraft(options.InitialDraft),
		stack:               []navigationFrame{{screen: ScreenWorkspace, draftBefore: Draft{}}},
	}
	for screen, picker := range model.pickers {
		picker.Screen = screen
		model.rememberResourceLabels(picker)
	}
	root, ok := model.pickers[ScreenWorkspace]
	if !ok {
		root = Picker{Screen: ScreenWorkspace, Title: "Workspaces", Dimension: "workspace"}
	}
	root.Screen = ScreenWorkspace
	root.ResourceBrowser, root.ModalActions = true, true
	model.stack[0].picker = root
	model.stack[0].focusOption(root.Focus)
	start := options.StartScreen
	if start == "" {
		start = ScreenWorkspace
	}
	if start != ScreenWorkspace {
		if picker, ok := model.pickers[start]; ok {
			if options.ResourceBrowser {
				model.stack = nil
			}
			model.push(picker, model.draft)
		}
	}
	if options.ResourceBrowser {
		if options.InitialDraft == nil && options.LaunchPreview == nil {
			model.seedResourceDraft()
			model.initialResourceDraft = cloneDraft(model.draft)
		}
		if model.current().picker.ResourceBrowser {
			if picker, ok := model.browserPicker(model.current().screen); ok {
				initial := model.current().picker
				picker.Focus = firstNonEmptyUI(initial.Focus, model.draft[model.current().screen])
				picker.Description = firstNonEmptyUI(initial.Description, picker.Description)
				model.replaceBrowserRoot(picker)
			}
		}
	}
	if options.InitialDialog != nil {
		model.push(*options.InitialDialog, model.draft)
	}
	return model
}

func (m *AppModel) seedResourceDraft() {
	if m.draft[ScreenIdentity] != "" || m.draft[ScreenProject] != "" || m.draft[ScreenKubernetes] != "" {
		return
	}
	observed := map[Screen]string{
		ScreenIdentity:   m.snapshot.Observed.Identity,
		ScreenProject:    m.snapshot.Observed.Project,
		ScreenKubernetes: m.snapshot.Observed.Kubernetes,
	}
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes} {
		value := observed[screen]
		if value == "" {
			continue
		}
		picker, ok := m.pickers[screen]
		if !ok {
			continue
		}
		for _, option := range picker.Options {
			matches := option.Name == value
			switch screen {
			case ScreenIdentity:
				matches = matches || option.IdentityAccount == value
			case ScreenProject:
				matches = matches || option.ProjectID == value
			case ScreenKubernetes:
				matches = matches || option.KubernetesContext == value || option.KubernetesCluster == value
			}
			if matches {
				m.draft[screen] = option.Name
				break
			}
		}
	}
}

// Run runs the only Bubble Tea program needed for a complete selection flow.
func Run(options AppOptions) (Outcome, error) {
	program := tea.NewProgram(NewAppModel(options))
	final, err := program.Run()
	if err != nil {
		return Outcome{}, err
	}
	model, ok := final.(AppModel)
	if !ok {
		return Outcome{}, nil
	}
	return model.outcome, nil
}

func (m AppModel) Init() tea.Cmd {
	commands := []tea.Cmd{}
	if m.readSharedConfig != nil {
		commands = append(commands, m.checkSharedConfig())
	}
	if m.probing {
		commands = append(commands, func() tea.Msg { return probeMessage(state.Probe(context.Background())) })
	}
	if m.refreshIdentityAuth != nil {
		commands = append(commands, m.checkIdentityAuth())
	}
	return tea.Batch(commands...)
}

func (m AppModel) checkIdentityAuth() tea.Cmd {
	refresh := m.refreshIdentityAuth
	return func() tea.Msg { refresh(); return identityAuthRefreshed{} }
}

type identityAuthRefreshed struct{}
type identityAuthTick struct{}

func (m AppModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case sharedConfigTick:
		return m, m.checkSharedConfig()
	case sharedConfigResult:
		m.snapshot.SharedConfigChecked = true
		m.snapshot.SharedConfig = message.manifest
		m.snapshot.SharedConfigError = ""
		if message.err != nil {
			m.snapshot.SharedConfigError = "Unavailable"
		}
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return sharedConfigTick{} })
	case identityAuthTick:
		if m.working {
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return identityAuthTick{} })
		}
		return m, m.checkIdentityAuth()
	case identityAuthRefreshed:
		if m.working {
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return identityAuthRefreshed{} })
		}
		if picker, ok := m.browserPicker(ScreenIdentity); ok {
			status := map[string]string{}
			for _, option := range picker.Options {
				status[option.Name] = option.AuthStatus
			}
			for index := range m.stack {
				if m.stack[index].screen == ScreenIdentity {
					for i := range m.stack[index].picker.Options {
						option := &m.stack[index].picker.Options[i]
						option.AuthStatus = status[option.Name]
					}
				}
			}
		}
		return m, tea.Tick(time.Minute, func(time.Time) tea.Msg { return identityAuthTick{} })
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		return m, nil
	case probeMessage:
		result := state.ProbeResult(message)
		m.snapshot.Observed = result.Observed
		m.probeErr = result.RelevantErrors(m.snapshot)
		m.probing, m.probed = false, true
		if m.snapshot.Managed {
			if mismatches := m.snapshot.Mismatches(); len(mismatches) > 0 {
				m.snapshot.LocalStatus = "CONTEXT-DRIFT"
				m.snapshot.Message = strings.Join(mismatches, "; ")
			} else {
				m.snapshot.LocalStatus = "LOCAL MATCH"
				m.snapshot.Message = "Expected and observed tool state agree"
			}
		}
		return m, nil
	case workProgress:
		if !m.working || message.id != m.workID {
			return m, nil
		}
		if !m.workStopping {
			m.workLabel = message.label
		}
		return m, waitWorkProgress(message.id, message.updates, message.done)
	case workTick:
		if m.working && message.id == m.workID {
			m.workFrame++
			return m, workingTick(m.workID)
		}
		return m, nil
	case workFinished:
		if !m.working || message.id != m.workID {
			return m, nil
		}
		m.working = false
		if m.workCancel != nil {
			m.workCancel()
			m.workCancel = nil
		}
		m.workStopping = false
		var updated tea.Model
		var cmd tea.Cmd
		if message.process {
			updated, cmd = m.applyProcessTransition(message.transition)
		} else {
			updated, cmd = m.applyChoiceTransition(message.transition, message.choice, message.before)
		}
		if m.quitAfterWork {
			return updated, tea.Quit
		}
		return updated, cmd
	case processFinishedMessage:
		if message.process != nil && message.process.Continue != nil {
			process := message.process
			return m.startWork(firstNonEmptyUI(process.Label, "Continuing after login"), func(ctx context.Context, report func(string)) Transition {
				return process.Continue(ctx, report, message.err)
			}, Choice{Option: Option{Cancellable: process.Cancellable}}, nil, true)
		}
		if m.asyncFlow && message.process != nil && message.process.Done != nil {
			return m.startWork(firstNonEmptyUI(message.process.Label, "Refreshing after authentication"), func(context.Context, func(string)) Transition { return message.process.Done(message.err) }, Choice{}, nil, true)
		}
		transition := Transition{}
		if message.process != nil && message.process.Done != nil {
			transition = message.process.Done(message.err)
		}
		return m.applyProcessTransition(transition)
	case tea.PasteMsg:
		if m.working || m.helpVisible {
			return m, nil
		}
		frame := &m.stack[len(m.stack)-1]
		if frame.picker.Input != nil {
			frame.insertInput(message.Content)
		} else if !frame.picker.HideSearch && (!frame.picker.ModalActions || frame.searching) {
			frame.filter += printableText(message.Content)
			frame.cursor, frame.cursorKey = 0, ""
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(message)
	default:
		return m, nil
	}
}

type sharedConfigTick struct{}
type sharedConfigResult struct {
	manifest *state.Manifest
	err      error
}

func (m AppModel) checkSharedConfig() tea.Cmd {
	if m.readSharedConfig == nil {
		return nil
	}
	read := m.readSharedConfig
	return func() tea.Msg { manifest, err := read(); return sharedConfigResult{manifest, err} }
}
