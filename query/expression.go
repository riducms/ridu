package query

import "fmt"

// ExpressionKind identifies the shape of an expression node.
type ExpressionKind string

const (
	ExpressionComparison ExpressionKind = "comparison"
	ExpressionAnd        ExpressionKind = "and"
	ExpressionOr         ExpressionKind = "or"
	ExpressionNot        ExpressionKind = "not"
)

// Operator identifies a field comparison.
type Operator string

const (
	OperatorEqual            Operator = "equal"
	OperatorNotEqual         Operator = "not_equal"
	OperatorIn               Operator = "in"
	OperatorExists           Operator = "exists"
	OperatorGreaterThan      Operator = "greater_than"
	OperatorGreaterThanEqual Operator = "greater_than_equal"
	OperatorLessThan         Operator = "less_than"
	OperatorLessThanEqual    Operator = "less_than_equal"
	OperatorContains         Operator = "contains"
	OperatorLike             Operator = "like"
)

// Comparison is a read-only snapshot of a comparison expression.
type Comparison struct {
	Path     Path
	Operator Operator
	Value    Value
}

// Node is a detached recursive snapshot for adapters and protocol encoders.
// Mutating a Node never mutates the originating Expression.
type Node struct {
	Kind       ExpressionKind
	Comparison *Comparison
	Children   []Node
}

// Expression is an immutable query expression. Implementations are sealed so
// every transport and store sees the same finite query language.
type Expression interface {
	Kind() ExpressionKind
	Node() Node
	expression()
}

type expression struct {
	kind       ExpressionKind
	comparison *Comparison
	children   []*expression
}

func (expression *expression) Kind() ExpressionKind { return expression.kind }
func (expression *expression) expression()          {}

func (expression *expression) Node() Node {
	return cloneNode(expression)
}

// Compare constructs a validated field comparison.
func Compare(path Path, operator Operator, value Value) (Expression, error) {
	if path.String() == "" {
		return nil, fmt.Errorf("comparison requires a non-empty field path")
	}
	switch operator {
	case OperatorEqual, OperatorNotEqual:
		if value.Kind() == ValueList {
			return nil, fmt.Errorf("operator %q does not accept a list value", operator)
		}
	case OperatorGreaterThan, OperatorGreaterThanEqual, OperatorLessThan, OperatorLessThanEqual:
		if value.Kind() != ValueString && value.Kind() != ValueNumber {
			return nil, fmt.Errorf("operator %q requires a string or number value", operator)
		}
	case OperatorContains, OperatorLike:
		if value.Kind() != ValueString {
			return nil, fmt.Errorf("operator %q requires a string value", operator)
		}
	case OperatorIn:
		if value.Kind() != ValueList {
			return nil, fmt.Errorf("operator %q requires a list value", operator)
		}
	case OperatorExists:
		if value.Kind() != ValueBoolean {
			return nil, fmt.Errorf("operator %q requires a boolean value", operator)
		}
	default:
		return nil, fmt.Errorf("unknown comparison operator %q", operator)
	}

	comparison := &Comparison{Path: clonePath(path), Operator: operator, Value: cloneValue(value)}
	return &expression{kind: ExpressionComparison, comparison: comparison}, nil
}

func GreaterThan(path Path, value Value) Expression {
	return mustCompare(path, OperatorGreaterThan, value)
}
func GreaterThanEqual(path Path, value Value) Expression {
	return mustCompare(path, OperatorGreaterThanEqual, value)
}
func LessThan(path Path, value Value) Expression { return mustCompare(path, OperatorLessThan, value) }
func LessThanEqual(path Path, value Value) Expression {
	return mustCompare(path, OperatorLessThanEqual, value)
}
func Contains(path Path, value string) Expression {
	return mustCompare(path, OperatorContains, String(value))
}
func Like(path Path, value string) Expression { return mustCompare(path, OperatorLike, String(value)) }

func mustCompare(path Path, operator Operator, value Value) Expression {
	expression, err := Compare(path, operator, value)
	if err != nil {
		panic(err)
	}
	return expression
}

// Equal constructs an equality expression.
func Equal(path Path, value Value) Expression {
	expression, err := Compare(path, OperatorEqual, value)
	if err != nil {
		panic(err)
	}
	return expression
}

// NotEqual constructs an inequality expression.
func NotEqual(path Path, value Value) Expression {
	expression, err := Compare(path, OperatorNotEqual, value)
	if err != nil {
		panic(err)
	}
	return expression
}

// In constructs a membership expression.
func In(path Path, values ...Value) Expression {
	expression, err := Compare(path, OperatorIn, List(values...))
	if err != nil {
		panic(err)
	}
	return expression
}

// And matches documents that satisfy every condition. With one condition it
// returns that condition. Like Equal, it panics when given no conditions or a
// nil one; check the length of a list built from input before calling it.
func And(conditions ...Expression) Expression {
	return logical(ExpressionAnd, conditions)
}

// Or matches documents that satisfy at least one condition. With one condition
// it returns that condition. It panics when given no conditions or a nil one.
func Or(conditions ...Expression) Expression {
	return logical(ExpressionOr, conditions)
}

// Not matches documents that do not satisfy condition. It panics when
// condition is nil.
func Not(condition Expression) Expression {
	if condition == nil {
		panic(fmt.Errorf("query.Not requires a condition"))
	}
	return &expression{kind: ExpressionNot, children: []*expression{expressionFromNode(condition.Node())}}
}

func logical(kind ExpressionKind, conditions []Expression) Expression {
	if len(conditions) == 0 {
		panic(fmt.Errorf("query.%s requires at least one condition", logicalName(kind)))
	}
	children := make([]*expression, len(conditions))
	for index, child := range conditions {
		if child == nil {
			panic(fmt.Errorf("query.%s condition %d is nil", logicalName(kind), index))
		}
		children[index] = expressionFromNode(child.Node())
	}
	if len(children) == 1 {
		return children[0]
	}
	return &expression{kind: kind, children: children}
}

func logicalName(kind ExpressionKind) string {
	if kind == ExpressionOr {
		return "Or"
	}
	return "And"
}

func cloneNode(source *expression) Node {
	node := Node{Kind: source.kind}
	if source.comparison != nil {
		comparison := *source.comparison
		comparison.Path = clonePath(comparison.Path)
		comparison.Value = cloneValue(comparison.Value)
		node.Comparison = &comparison
	}
	if source.children != nil {
		node.Children = make([]Node, len(source.children))
		for index, child := range source.children {
			node.Children[index] = cloneNode(child)
		}
	}
	return node
}

func expressionFromNode(node Node) *expression {
	result := &expression{kind: node.Kind}
	if node.Comparison != nil {
		comparison := *node.Comparison
		comparison.Path = clonePath(comparison.Path)
		comparison.Value = cloneValue(comparison.Value)
		result.comparison = &comparison
	}
	if node.Children != nil {
		result.children = make([]*expression, len(node.Children))
		for index, child := range node.Children {
			result.children[index] = expressionFromNode(child)
		}
	}
	return result
}

func clonePath(path Path) Path {
	return Path{segments: path.Segments()}
}

func cloneValue(value Value) Value {
	value.items = cloneValues(value.items)
	return value
}
