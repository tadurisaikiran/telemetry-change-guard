// Package graph provides the deterministic in-memory dependency graph used by
// impact and readiness analysis.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

// NodeKind identifies the role of a graph node.
type NodeKind string

const (
	NodeKindSymbol   NodeKind = "telemetry_symbol"
	NodeKindConsumer NodeKind = "consumer"
)

// EdgeKind identifies the dependency relationship between graph nodes. Edges
// point in impact-propagation order.
type EdgeKind string

const (
	EdgeReferences  EdgeKind = "references"
	EdgeProduces    EdgeKind = "produces"
	EdgeContains    EdgeKind = "contains"
	EdgeDerivedFrom EdgeKind = "derived_from"
	EdgeGeneratedBy EdgeKind = "generated_by"
	EdgeOwnedBy     EdgeKind = "owned_by"
)

// Node is one symbol or consumer in the dependency graph.
type Node struct {
	ID       string
	Kind     NodeKind
	Name     string
	Symbol   *domain.Symbol
	Consumer *domain.Consumer
}

// Edge is a directed dependency relationship.
type Edge struct {
	From string
	To   string
	Kind EdgeKind
}

// Path is one deterministic impact path beginning at a requested node.
type Path struct {
	Nodes []string
	Edges []EdgeKind
}

// Graph is a custom, deterministic directed graph. It requires no database and
// is rebuilt for each analysis.
type Graph struct {
	nodes map[string]Node
	out   map[string][]Edge
	in    map[string][]Edge
	edges map[string]struct{}
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		nodes: make(map[string]Node),
		out:   make(map[string][]Edge),
		in:    make(map[string][]Edge),
		edges: make(map[string]struct{}),
	}
}

// AddNode adds node or accepts an identical existing node.
func (graph *Graph) AddNode(node Node) error {
	if node.ID == "" {
		return fmt.Errorf("graph node ID is required")
	}
	if existing, exists := graph.nodes[node.ID]; exists {
		if existing.Kind != node.Kind || existing.Name != node.Name {
			return fmt.Errorf("graph node %q conflicts with an existing node", node.ID)
		}
		return nil
	}
	graph.nodes[node.ID] = node
	return nil
}

// AddEdge adds a deduplicated directed edge. Both endpoints must exist.
func (graph *Graph) AddEdge(edge Edge) error {
	if _, exists := graph.nodes[edge.From]; !exists {
		return fmt.Errorf("edge source node %q does not exist", edge.From)
	}
	if _, exists := graph.nodes[edge.To]; !exists {
		return fmt.Errorf("edge destination node %q does not exist", edge.To)
	}
	key := edgeKey(edge)
	if _, exists := graph.edges[key]; exists {
		return nil
	}
	graph.edges[key] = struct{}{}
	graph.out[edge.From] = append(graph.out[edge.From], edge)
	graph.in[edge.To] = append(graph.in[edge.To], edge)
	return nil
}

// Node returns a graph node by ID.
func (graph *Graph) Node(id string) (Node, bool) {
	node, ok := graph.nodes[id]
	return node, ok
}

// Nodes returns every node in stable ID order.
func (graph *Graph) Nodes() []Node {
	nodes := make([]Node, 0, len(graph.nodes))
	for _, node := range graph.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// Edges returns every edge in stable order.
func (graph *Graph) Edges() []Edge {
	edges := make([]Edge, 0, len(graph.edges))
	for _, outgoing := range graph.out {
		edges = append(edges, outgoing...)
	}
	sortEdges(edges)
	return edges
}

// Dependents returns nodes directly impacted by id.
func (graph *Graph) Dependents(id string) []Node {
	edges := append([]Edge(nil), graph.out[id]...)
	sortEdges(edges)
	result := make([]Node, 0, len(edges))
	for _, edge := range edges {
		result = append(result, graph.nodes[edge.To])
	}
	return result
}

// Dependencies returns nodes that directly feed id.
func (graph *Graph) Dependencies(id string) []Node {
	edges := append([]Edge(nil), graph.in[id]...)
	sortEdges(edges)
	result := make([]Node, 0, len(edges))
	for _, edge := range edges {
		result = append(result, graph.nodes[edge.From])
	}
	return result
}

// ImpactPaths performs a deterministic breadth-first traversal. It records the
// shortest path to every reachable node and terminates safely on cycles.
func (graph *Graph) ImpactPaths(start string) []Path {
	if _, exists := graph.nodes[start]; !exists {
		return nil
	}

	type queueItem struct {
		node string
		path Path
	}
	queue := []queueItem{{node: start, path: Path{Nodes: []string{start}}}}
	visited := map[string]struct{}{start: {}}
	var paths []Path

	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]

		edges := append([]Edge(nil), graph.out[current.node]...)
		sortEdges(edges)
		for _, edge := range edges {
			if _, exists := visited[edge.To]; exists {
				continue
			}
			visited[edge.To] = struct{}{}
			nextPath := Path{
				Nodes: appendCopy(current.path.Nodes, edge.To),
				Edges: appendCopy(current.path.Edges, edge.Kind),
			}
			paths = append(paths, nextPath)
			queue = append(queue, queueItem{node: edge.To, path: nextPath})
		}
	}

	return paths
}

// SymbolNodeID returns the stable graph ID for a telemetry symbol.
func SymbolNodeID(symbol domain.Symbol) string {
	return strings.Join([]string{
		"symbol",
		string(symbol.Domain),
		string(symbol.Kind),
		symbol.Parent,
		symbol.Name,
	}, ":")
}

// ConsumerNodeID returns the stable graph ID for a normalized consumer.
func ConsumerNodeID(consumerID string) string {
	return "consumer:" + consumerID
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
}

func edgeKey(edge Edge) string {
	return strings.Join([]string{edge.From, edge.To, string(edge.Kind)}, "\x00")
}

func appendCopy[T any](values []T, value T) []T {
	copyOfValues := make([]T, len(values), len(values)+1)
	copy(copyOfValues, values)
	return append(copyOfValues, value)
}
