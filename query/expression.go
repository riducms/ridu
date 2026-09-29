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

// Compare constructs a validated field comparison from an operator chosen at
// run time, such as one read from a request. It returns an error where the
// helpers below panic.
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

	// Paths and values cannot be changed after construction, so the comparison
	// keeps them without copying.
	comparison := &Comparison{Path: path, Operator: operator, Value: value}
	return &expression{kind: ExpressionComparison, comparison: comparison}, nil
}

func mustCompare(path Path, operator Operator, value Value) Expression {
	expression, err := Compare(path, operator, value)
	if err != nil {
		panic(err)
	}
	return expression
}

// Equal matches documents whose field equals value, as in
// Equal("status", "published") or Equal("parent", query.Null()).
func Equal[P FieldPath, V Operand](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorEqual, valueOf(value))
}

// NotEqual matches documents whose field differs from value.
func NotEqual[P FieldPath, V Operand](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorNotEqual, valueOf(value))
}

// In matches documents whose field equals one of values, as in
// In("status", "draft", "published"). With no values it matches nothing.
func In[P FieldPath, V Operand](path P, values ...V) Expression {
	items := make([]Value, len(values))
	for index, value := range values {
		items[index] = valueOf(value)
	}
	return mustCompare(pathOf(path), OperatorIn, Value{kind: ValueList, items: items})
}

// GreaterThan matches documents whose field is greater than value.
func GreaterThan[P FieldPath, V Ordered](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorGreaterThan, valueOf(value))
}

// GreaterThanEqual matches documents whose field is at least value.
func GreaterThanEqual[P FieldPath, V Ordered](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorGreaterThanEqual, valueOf(value))
}

// LessThan matches documents whose field is less than value.
func LessThan[P FieldPath, V Ordered](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorLessThan, valueOf(value))
}

// LessThanEqual matches documents whose field is at most value.
func LessThanEqual[P FieldPath, V Ordered](path P, value V) Expression {
	return mustCompare(pathOf(path), OperatorLessThanEqual, valueOf(value))
}

// Contains matches documents whose text field contains value, ignoring case.
func Contains[P FieldPath](path P, value string) Expression {
	return mustCompare(pathOf(path), OperatorContains, String(value))
}

// Like matches documents whose text field contains every word of value,
// ignoring case and word order.
func Like[P FieldPath](path P, value string) Expression {
	return mustCompare(pathOf(path), OperatorLike, String(value))
}

// Exists matches documents that have a value for the field when exists is
// true, and documents without one when it is false.
func Exists[P FieldPath](path P, exists bool) Expression {
	return mustCompare(pathOf(path), OperatorExists, Boolean(exists))
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
	return &expression{kind: ExpressionNot, children: []*expression{sealed(condition)}}
}

// logical shares immutable child nodes and merges nested conditions of the same
// kind: And(And(a, b), c) is And(a, b, c). Flattening copies child pointers into
// a new slice; it does not copy the nodes themselves.
func logical(kind ExpressionKind, conditions []Expression) Expression {
	if len(conditions) == 0 {
		panic(fmt.Errorf("query.%s requires at least one condition", logicalName(kind)))
	}
	children := make([]*expression, 0, len(conditions))
	for index, condition := range conditions {
		if condition == nil {
			panic(fmt.Errorf("query.%s condition %d is nil", logicalName(kind), index))
		}
		child := sealed(condition)
		if child.kind == kind {
			children = append(children, child.children...)
			continue
		}
		children = append(children, child)
	}
	if len(children) == 1 {
		return children[0]
	}
	return &expression{kind: kind, children: children}
}

// sealed returns the package's own representation of an expression. Another
// type can satisfy Expression only by embedding one, so it is copied once.
func sealed(condition Expression) *expression {
	if own, ok := condition.(*expression); ok {
		return own
	}
	return expressionFromNode(condition.Node())
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
		// Path and Value are immutable, so the snapshot copies only the struct.
		comparison := *source.comparison
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
