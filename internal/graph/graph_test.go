package graph

import "testing"

func TestImpactPathsTerminatesOnCycle(t *testing.T) {
	t.Parallel()

	target := New()
	for _, node := range []Node{
		{ID: "raw", Kind: NodeKindSymbol, Name: "raw"},
		{ID: "rule-a", Kind: NodeKindConsumer, Name: "rule-a"},
		{ID: "derived", Kind: NodeKindSymbol, Name: "derived"},
	} {
		if err := target.AddNode(node); err != nil {
			t.Fatalf("AddNode(%q) error = %v", node.ID, err)
		}
	}
	for _, edge := range []Edge{
		{From: "raw", To: "rule-a", Kind: EdgeReferences},
		{From: "rule-a", To: "derived", Kind: EdgeProduces},
		{From: "derived", To: "rule-a", Kind: EdgeReferences},
	} {
		if err := target.AddEdge(edge); err != nil {
			t.Fatalf("AddEdge(%+v) error = %v", edge, err)
		}
	}

	paths := target.ImpactPaths("raw")
	if got, want := len(paths), 2; got != want {
		t.Fatalf("len(ImpactPaths) = %d, want %d; paths = %+v", got, want, paths)
	}
	if got, want := paths[1].Nodes, []string{"raw", "rule-a", "derived"}; !equalStrings(got, want) {
		t.Errorf("final path = %v, want %v", got, want)
	}
}

func TestGraphRejectsMissingEdgeEndpoint(t *testing.T) {
	t.Parallel()

	target := New()
	if err := target.AddNode(Node{ID: "present", Kind: NodeKindSymbol}); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}
	if err := target.AddEdge(Edge{From: "present", To: "missing", Kind: EdgeReferences}); err == nil {
		t.Fatal("AddEdge() error = nil, want missing-endpoint error")
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
