package ui

// resourceModule translates one resource kind into table data. Layout,
// scrolling, borders, and footer placement remain owned by the display shell.
type resourceModule interface {
	Columns() []string
	Values(Option) []string
}

type identityModule struct{}

func (identityModule) Columns() []string {
	return []string{"IDENTITY", "PROVIDER", "AUTH", "TAGS", "SOURCE"}
}
func (identityModule) Values(option Option) []string {
	return []string{option.IdentityAccount, option.Provider, option.AuthStatus, resourceLabels(option), option.Source}
}

type projectModule struct{}

func (projectModule) Columns() []string {
	return []string{"PROJECT ID", "IDENTITY", "KUBERNETES", "TAGS", "SOURCE"}
}
func (projectModule) Values(option Option) []string {
	return []string{option.ProjectID, option.IdentityAccount, option.KubernetesContext, resourceLabels(option), option.Source}
}

type kubernetesModule struct{}

func (kubernetesModule) Columns() []string {
	return []string{"CLUSTER", "CONTEXT", "PROJECT ID", "IDENTITY", "TAGS", "SOURCE"}
}
func (kubernetesModule) Values(option Option) []string {
	name := firstNonEmptyUI(option.KubernetesCluster, option.KubernetesContext, option.Name)
	return []string{
		name,
		option.KubernetesContext, option.ProjectID, option.IdentityAccount, resourceLabels(option), option.Source,
	}
}

type dockerModule struct{}

func (dockerModule) Columns() []string { return []string{"DOCKER", "TAGS", "SOURCE"} }
func (dockerModule) Values(option Option) []string {
	return []string{option.DockerContext, resourceLabels(option), option.Source}
}

type workspaceModule struct{}

func (workspaceModule) Columns() []string {
	return []string{"WORKSPACE", "PROJECT / TARGET", "IDENTITY", "TAGS", "SOURCE"}
}
func (workspaceModule) Values(option Option) []string {
	target := optionScope(option)
	if target == "" {
		target = option.Detail
	}
	return []string{
		firstNonEmptyUI(option.Label, option.Name), target, option.IdentityAccount, resourceLabels(option), option.Source,
	}
}

func moduleForDimension(dimension string) resourceModule {
	return tabFor(Screen(dimension)).module
}

func resourceLabels(option Option) string { return RenderTags(option.Tags) }
