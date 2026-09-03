package store

import (
	"errors"
	"testing"
)

func TestPopulationBudgetCountsTheExpandedDocumentTree(t *testing.T) {
	leaf := Document{ID: "leaf", Values: Values{"title": String("Leaf")}}
	parent := Document{ID: "parent", Values: Values{
		"children": List(Populated(leaf), Populated(leaf)),
	}}
	budget := NewPopulationBudget(4)
	if err := budget.ConsumeDocument(parent); err != nil {
		t.Fatalf("consume three-node tree: %v", err)
	}
	if err := budget.ConsumeDocument(leaf); err != nil {
		t.Fatalf("consume final node: %v", err)
	}
	if err := budget.ConsumeDocument(leaf); !errors.Is(err, ErrPopulationLimit) {
		t.Fatalf("consume past limit error = %v, want ErrPopulationLimit", err)
	}
}

func TestPopulationBudgetRejectsNestedTreeBeforeReservation(t *testing.T) {
	leaf := Document{ID: "leaf", Values: Values{}}
	parent := Document{ID: "parent", Values: Values{"child": Populated(leaf)}}
	budget := NewPopulationBudget(1)
	if err := budget.ConsumeDocument(parent); !errors.Is(err, ErrPopulationLimit) {
		t.Fatalf("nested tree error = %v, want ErrPopulationLimit", err)
	}
	if err := budget.ConsumeDocument(leaf); err != nil {
		t.Fatalf("failed reservation consumed budget: %v", err)
	}
}
