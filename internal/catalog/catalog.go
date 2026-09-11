// Package catalog builds dependency views and safe, previewable configuration
// mutation plans. It deliberately has no terminal UI dependencies.
package catalog

import (
	"sort"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

type Kind string

const (
	KindIdentity   Kind = "identity"
	KindProject    Kind = "project"
	KindKubernetes Kind = "kubernetes"
	KindDocker     Kind = "docker"
	KindWorkspace  Kind = "workspace"
)

type Ref struct {
	Kind Kind
	Name string
}

type Relation string

const (
	IdentityAccess Relation = "identity_access"
	CloudProject   Relation = "cloud_project"
	UsesComponent  Relation = "uses_component"
)

type Node struct {
	Ref      Ref
	Label    string
	Provider string
	Risk     string
}

type Edge struct {
	From     Ref
	To       Ref
	Relation Relation
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}

type TreeNode struct {
	Node     Node
	Children []TreeNode
}

func Build(cfg config.Config) Graph {
	graph := Graph{}
	for _, name := range sortedKeys(cfg.Identities) {
		item := cfg.Identities[name]
		graph.Nodes = append(graph.Nodes, Node{Ref: Ref{KindIdentity, name}, Label: item.Account, Provider: item.Provider})
	}
	for _, name := range sortedKeys(cfg.Projects) {
		item := cfg.Projects[name]
		projectRef := Ref{KindProject, name}
		graph.Nodes = append(graph.Nodes, Node{Ref: projectRef, Label: item.ProjectID, Provider: item.Provider, Risk: item.Risk})
		for _, identity := range resolver.EligibleIdentities(cfg, name, "") {
			graph.Edges = append(graph.Edges, Edge{From: Ref{KindIdentity, identity}, To: projectRef, Relation: IdentityAccess})
		}
	}
	for _, name := range sortedKeys(cfg.Kubernetes) {
		item := cfg.Kubernetes[name]
		ref := Ref{KindKubernetes, name}
		label := item.Cluster
		if label == "" {
			label = item.Context
		}
		if label == "" {
			label = name
		}
		graph.Nodes = append(graph.Nodes, Node{Ref: ref, Label: label, Risk: item.Risk})
		if item.Project != "" {
			graph.Edges = append(graph.Edges, Edge{From: Ref{KindProject, item.Project}, To: ref, Relation: CloudProject})
		}
	}
	for _, name := range sortedKeys(cfg.Docker) {
		item := cfg.Docker[name]
		graph.Nodes = append(graph.Nodes, Node{Ref: Ref{KindDocker, name}, Label: item.Context, Risk: item.Risk})
	}
	for _, name := range sortedKeys(cfg.Destinations) {
		item := cfg.Destinations[name]
		workspaceRef := Ref{KindWorkspace, name}
		graph.Nodes = append(graph.Nodes, Node{Ref: workspaceRef, Label: name, Risk: item.Risk})
		for _, component := range workspaceComponents(item) {
			graph.Edges = append(graph.Edges, Edge{From: component, To: workspaceRef, Relation: UsesComponent})
		}
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		left, right := graph.Edges[i], graph.Edges[j]
		if left.From.Kind != right.From.Kind {
			return left.From.Kind < right.From.Kind
		}
		if left.From.Name != right.From.Name {
			return left.From.Name < right.From.Name
		}
		if left.To.Kind != right.To.Kind {
			return left.To.Kind < right.To.Kind
		}
		return left.To.Name < right.To.Name
	})
	return graph
}

// IdentityTree returns identity -> project -> KindKubernetes trees, followed by
// unmapped projects and KindKubernetes targets as separate roots.
func IdentityTree(cfg config.Config) []TreeNode {
	nodes := nodeIndex(Build(cfg))
	projectsByIdentity := map[string][]string{}
	for name := range cfg.Projects {
		for _, identity := range resolver.EligibleIdentities(cfg, name, "") {
			projectsByIdentity[identity] = append(projectsByIdentity[identity], name)
		}
	}
	kubernetesByProject := map[string][]string{}
	for name, target := range cfg.Kubernetes {
		kubernetesByProject[target.Project] = append(kubernetesByProject[target.Project], name)
	}
	var roots []TreeNode
	for _, identity := range sortedKeys(cfg.Identities) {
		root := TreeNode{Node: nodes[Ref{KindIdentity, identity}]}
		for _, project := range sortedStrings(projectsByIdentity[identity]) {
			targets := []string{}
			for _, target := range kubernetesByProject[project] {
				if resolver.KubernetesIdentityAvailable(cfg, target, identity) {
					targets = append(targets, target)
				}
			}
			root.Children = append(root.Children, projectBranch(nodes, project, targets))
		}
		roots = append(roots, root)
	}
	for _, project := range sortedKeys(cfg.Projects) {
		if len(resolver.EligibleIdentities(cfg, project, "")) == 0 {
			roots = append(roots, projectBranch(nodes, project, kubernetesByProject[project]))
		}
	}
	for _, target := range sortedStrings(kubernetesByProject[""]) {
		roots = append(roots, TreeNode{Node: nodes[Ref{KindKubernetes, target}]})
	}
	return roots
}

// Reverse returns the selected node and its direct incoming dependencies and
// outgoing dependants in deterministic order.
func Reverse(cfg config.Config, selected Ref) TreeNode {
	graph := Build(cfg)
	nodes := nodeIndex(graph)
	root := TreeNode{Node: nodes[selected]}
	seen := map[Ref]bool{}
	for _, edge := range graph.Edges {
		var related Ref
		switch {
		case edge.From == selected:
			related = edge.To
		case edge.To == selected:
			related = edge.From
		default:
			continue
		}
		if !seen[related] {
			seen[related] = true
			root.Children = append(root.Children, TreeNode{Node: nodes[related]})
		}
	}
	sort.Slice(root.Children, func(i, j int) bool {
		left, right := root.Children[i].Node.Ref, root.Children[j].Node.Ref
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Name < right.Name
	})
	return root
}

func projectBranch(nodes map[Ref]Node, project string, targets []string) TreeNode {
	branch := TreeNode{Node: nodes[Ref{KindProject, project}]}
	for _, target := range sortedStrings(targets) {
		branch.Children = append(branch.Children, TreeNode{Node: nodes[Ref{KindKubernetes, target}]})
	}
	return branch
}

func nodeIndex(graph Graph) map[Ref]Node {
	result := make(map[Ref]Node, len(graph.Nodes))
	for _, node := range graph.Nodes {
		result[node.Ref] = node
	}
	return result
}

func workspaceComponents(item config.Destination) []Ref {
	var result []Ref
	for _, candidate := range []Ref{{KindIdentity, item.Identity}, {KindProject, item.Project}, {KindKubernetes, item.Kubernetes}, {KindDocker, item.Docker}} {
		if candidate.Name != "" {
			result = append(result, candidate)
		}
	}
	return result
}

func sortedKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return sortedStrings(result)
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
