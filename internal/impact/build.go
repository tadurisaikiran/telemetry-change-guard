// Package impact constructs and traverses dependency graphs for telemetry
// changes.
package impact

import (
	"fmt"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
)

// BuildGraph converts normalized adapter discovery into an impact-propagation
// graph. Confirmed and unresolved references are both retained; readiness
// policy decides how their evidence may be used.
func BuildGraph(discovery domain.Discovery) (*graph.Graph, error) {
	result := graph.New()

	for index := range discovery.Consumers {
		consumer := discovery.Consumers[index]
		consumerCopy := consumer
		if err := result.AddNode(graph.Node{
			ID:       graph.ConsumerNodeID(consumer.ID),
			Kind:     graph.NodeKindConsumer,
			Name:     consumer.Name,
			Consumer: &consumerCopy,
		}); err != nil {
			return nil, fmt.Errorf("add consumer %q: %w", consumer.ID, err)
		}
	}

	for _, reference := range discovery.References {
		if err := addSymbolNode(result, reference.Symbol); err != nil {
			return nil, err
		}
		consumerID := graph.ConsumerNodeID(reference.ConsumerID)
		if _, exists := result.Node(consumerID); !exists {
			return nil, fmt.Errorf("reference consumer %q was not discovered", reference.ConsumerID)
		}
		if err := result.AddEdge(graph.Edge{
			From: graph.SymbolNodeID(reference.Symbol),
			To:   consumerID,
			Kind: graph.EdgeReferences,
		}); err != nil {
			return nil, fmt.Errorf("add reference edge for %q: %w", reference.ConsumerID, err)
		}
	}

	for _, production := range discovery.Productions {
		if err := addSymbolNode(result, production.Symbol); err != nil {
			return nil, err
		}
		consumerID := graph.ConsumerNodeID(production.ConsumerID)
		if _, exists := result.Node(consumerID); !exists {
			return nil, fmt.Errorf("production consumer %q was not discovered", production.ConsumerID)
		}
		if err := result.AddEdge(graph.Edge{
			From: consumerID,
			To:   graph.SymbolNodeID(production.Symbol),
			Kind: graph.EdgeProduces,
		}); err != nil {
			return nil, fmt.Errorf("add production edge for %q: %w", production.ConsumerID, err)
		}
	}

	return result, nil
}

func addSymbolNode(target *graph.Graph, symbol domain.Symbol) error {
	symbolCopy := symbol
	if err := target.AddNode(graph.Node{
		ID:     graph.SymbolNodeID(symbol),
		Kind:   graph.NodeKindSymbol,
		Name:   symbol.Name,
		Symbol: &symbolCopy,
	}); err != nil {
		return fmt.Errorf("add symbol %q: %w", symbol.Name, err)
	}
	return nil
}
