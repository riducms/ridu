package store

import "sync"

// MaxPopulationMaterializedDocuments bounds the total populated document
// nodes that an official adapter may construct for one response. It limits
// the expanded response tree, rather than only the number of distinct rows,
// because one densely connected row can otherwise be duplicated
// exponentially at each population depth.
const MaxPopulationMaterializedDocuments = 4096

// PopulationBudget is a request-scoped, concurrency-safe population output
// budget. Adapters share one instance across every recursive population read.
type PopulationBudget struct {
	mu        sync.Mutex
	remaining int
}

// NewPopulationBudget creates a materialization budget. Non-positive limits
// fail closed: the first populated document exceeds the budget.
func NewPopulationBudget(limit int) *PopulationBudget {
	return &PopulationBudget{remaining: max(limit, 0)}
}

// ConsumeDocument reserves enough budget for document and every populated
// document already nested beneath it. The reservation happens before an
// adapter deep-clones the document into its parent response.
func (budget *PopulationBudget) ConsumeDocument(document Document) error {
	if budget == nil {
		return ErrPopulationLimit
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	weight, exceeded := populationDocumentWeight(document, budget.remaining)
	if exceeded || weight > budget.remaining {
		return ErrPopulationLimit
	}
	budget.remaining -= weight
	return nil
}

func populationDocumentWeight(document Document, ceiling int) (int, bool) {
	weight := 1
	if weight > ceiling {
		return weight, true
	}
	var visitValue func(Value) bool
	visitValues := func(values Values) bool {
		for _, value := range values {
			if !visitValue(value) {
				return false
			}
		}
		return true
	}
	visitValue = func(value Value) bool {
		switch value.kind {
		case ValueObject:
			return visitValues(value.object)
		case ValueDocument:
			if value.document == nil {
				return true
			}
			weight++
			if weight > ceiling {
				return false
			}
			return visitValues(value.document.Values)
		case ValueList:
			for _, item := range value.list {
				if !visitValue(item) {
					return false
				}
			}
		}
		return true
	}
	return weight, !visitValues(document.Values)
}
