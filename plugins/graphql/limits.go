package graphql

import (
	"strings"

	"github.com/graphql-go/graphql/language/ast"
)

const (
	hardMaxGraphQLDocumentTokens     = 131_072
	hardMaxGraphQLDocumentNodes      = 65_536
	hardMaxGraphQLFragments          = 1_024
	hardMaxGraphQLFragmentExpansions = 32_768
	hardMaxGraphQLSyntaxDepth        = 256
	hardMaxGraphQLFragmentDepth      = 128
)

// graphQLDocumentLimits bound work that happens before the configured field
// complexity budget can be evaluated. The generous scaling factors preserve
// ordinary argument-, directive-, and fragment-heavy documents while the hard
// ceilings keep a permissive application configuration from disabling parser
// and validator safety.
type graphQLDocumentLimits struct {
	maxTokens             int
	maxNodes              int
	maxFragments          int
	maxFragmentExpansions int
	maxSyntaxDepth        int
	maxFragmentDepth      int
}

func documentLimitsFor(options Options) graphQLDocumentLimits {
	limits := graphQLDocumentLimits{
		maxTokens:             scaledGraphQLLimit(4_096, hardMaxGraphQLDocumentTokens, options.MaxComplexity, 128, options.MaxAliases, 16, options.MaxDepth, 32),
		maxNodes:              scaledGraphQLLimit(2_048, hardMaxGraphQLDocumentNodes, options.MaxComplexity, 16, options.MaxAliases, 8, options.MaxDepth, 16),
		maxFragments:          scaledGraphQLLimit(64, hardMaxGraphQLFragments, options.MaxComplexity, 1, options.MaxAliases, 1, options.MaxDepth, 1),
		maxFragmentExpansions: scaledGraphQLLimit(2_048, hardMaxGraphQLFragmentExpansions, options.MaxComplexity, 8, options.MaxAliases, 8, options.MaxDepth, 16),
		maxSyntaxDepth:        scaledGraphQLLimit(64, hardMaxGraphQLSyntaxDepth, options.MaxDepth, 4),
		maxFragmentDepth:      scaledGraphQLLimit(32, hardMaxGraphQLFragmentDepth, options.MaxDepth, 4),
	}
	// A token cannot occupy fewer than one request byte. Keeping the token
	// budget below the transport budget makes a deliberately small body limit
	// authoritative without narrowing any document that could fit in it.
	if options.MaxBodyBytes > 0 && options.MaxBodyBytes < int64(limits.maxTokens) {
		limits.maxTokens = int(options.MaxBodyBytes)
		if limits.maxTokens < 1 {
			limits.maxTokens = 1
		}
	}
	return limits
}

func scaledGraphQLLimit(minimum, maximum int, terms ...int) int {
	total := int64(minimum)
	for index := 0; index+1 < len(terms); index += 2 {
		value, scale := terms[index], terms[index+1]
		if value <= 0 || scale <= 0 {
			continue
		}
		if value > maximum/scale {
			return maximum
		}
		addition := int64(value) * int64(scale)
		if addition > int64(maximum) || total > int64(maximum)-addition {
			return maximum
		}
		total += addition
	}
	if total > int64(maximum) {
		return maximum
	}
	return int(total)
}

// guardGraphQLSource performs a deliberately small lexical pass before the
// recursive third-party parser sees the document. Braces in strings, block
// strings, and comments are ignored, so the guard measures syntax rather than
// bytes that merely resemble syntax.
func guardGraphQLSource(queryText string, limits graphQLDocumentLimits) error {
	tokens := 0
	delimiters := make([]byte, 0, limits.maxSyntaxDepth)
	addToken := func() error {
		tokens++
		if tokens > limits.maxTokens {
			return limitError("graphql_document_complexity_exceeded", "GraphQL document exceeds the safe token limit", tokens, limits.maxTokens)
		}
		return nil
	}

	for index := 0; index < len(queryText); {
		current := queryText[index]
		switch {
		case current == ' ' || current == '\t' || current == '\r' || current == '\n' || current == ',':
			index++
		case current == 0xef && index+2 < len(queryText) && queryText[index+1] == 0xbb && queryText[index+2] == 0xbf:
			index += 3
		case current == '#':
			index++
			for index < len(queryText) && queryText[index] != '\r' && queryText[index] != '\n' {
				index++
			}
		case current == '"':
			if err := addToken(); err != nil {
				return err
			}
			if strings.HasPrefix(queryText[index:], `"""`) {
				index += 3
				for index < len(queryText) {
					if queryText[index] == '\\' && index+3 < len(queryText) && strings.HasPrefix(queryText[index+1:], `"""`) {
						index += 4
						continue
					}
					if strings.HasPrefix(queryText[index:], `"""`) {
						index += 3
						break
					}
					index++
				}
				continue
			}
			index++
			for index < len(queryText) {
				if queryText[index] == '\\' {
					index += 2
					if index > len(queryText) {
						index = len(queryText)
					}
					continue
				}
				if queryText[index] == '"' {
					index++
					break
				}
				index++
			}
		case isGraphQLNameStart(current):
			if err := addToken(); err != nil {
				return err
			}
			index++
			for index < len(queryText) && isGraphQLNameContinue(queryText[index]) {
				index++
			}
		case current == '-' || current >= '0' && current <= '9':
			if err := addToken(); err != nil {
				return err
			}
			index = scanGraphQLNumber(queryText, index)
		case current == '(' || current == '[' || current == '{':
			if err := addToken(); err != nil {
				return err
			}
			delimiters = append(delimiters, current)
			if len(delimiters) > limits.maxSyntaxDepth {
				return limitError("graphql_document_depth_exceeded", "GraphQL document nesting exceeds the safe limit", len(delimiters), limits.maxSyntaxDepth)
			}
			index++
		case current == ')' || current == ']' || current == '}':
			if err := addToken(); err != nil {
				return err
			}
			if len(delimiters) != 0 && matchingGraphQLDelimiter(delimiters[len(delimiters)-1], current) {
				delimiters = delimiters[:len(delimiters)-1]
			}
			index++
		case current == '.' && strings.HasPrefix(queryText[index:], "..."):
			if err := addToken(); err != nil {
				return err
			}
			index += 3
		default:
			if err := addToken(); err != nil {
				return err
			}
			index++
		}
	}
	return nil
}

func isGraphQLNameStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isGraphQLNameContinue(value byte) bool {
	return isGraphQLNameStart(value) || value >= '0' && value <= '9'
}

func scanGraphQLNumber(queryText string, index int) int {
	if queryText[index] == '-' {
		index++
	}
	if index < len(queryText) && queryText[index] == '0' {
		index++
	} else {
		for index < len(queryText) && queryText[index] >= '0' && queryText[index] <= '9' {
			index++
		}
	}
	if index < len(queryText) && queryText[index] == '.' && index+1 < len(queryText) && queryText[index+1] >= '0' && queryText[index+1] <= '9' {
		index += 2
		for index < len(queryText) && queryText[index] >= '0' && queryText[index] <= '9' {
			index++
		}
	}
	if index < len(queryText) && (queryText[index] == 'e' || queryText[index] == 'E') {
		candidate := index + 1
		if candidate < len(queryText) && (queryText[candidate] == '+' || queryText[candidate] == '-') {
			candidate++
		}
		if candidate < len(queryText) && queryText[candidate] >= '0' && queryText[candidate] <= '9' {
			index = candidate + 1
			for index < len(queryText) && queryText[index] >= '0' && queryText[index] <= '9' {
				index++
			}
		}
	}
	return index
}

func matchingGraphQLDelimiter(open, close byte) bool {
	return open == '(' && close == ')' || open == '[' && close == ']' || open == '{' && close == '}'
}

type graphQLSelectionInspection struct {
	selectionSet *ast.SelectionSet
	fragmentName string
	depth        int
}

type graphQLDocumentInspector struct {
	limits         graphQLDocumentLimits
	nodes          int
	fragments      int
	fragmentGraph  map[string][]string
	selectionStack []graphQLSelectionInspection
	valueStack     []ast.Value
}

func inspectGraphQLDocument(document *ast.Document, limits graphQLDocumentLimits) (map[string][]string, error) {
	inspector := &graphQLDocumentInspector{limits: limits, fragmentGraph: make(map[string][]string)}
	if document == nil {
		return inspector.fragmentGraph, nil
	}
	if err := inspector.addNodes(len(document.Definitions)); err != nil {
		return nil, err
	}
	for _, definition := range document.Definitions {
		switch current := definition.(type) {
		case *ast.OperationDefinition:
			if err := inspector.addNodes(len(current.VariableDefinitions)); err != nil {
				return nil, err
			}
			for _, variable := range current.VariableDefinitions {
				if variable != nil && variable.DefaultValue != nil {
					inspector.valueStack = append(inspector.valueStack, variable.DefaultValue)
				}
			}
			if err := inspector.addDirectives(current.Directives); err != nil {
				return nil, err
			}
			if current.SelectionSet != nil {
				inspector.selectionStack = append(inspector.selectionStack, graphQLSelectionInspection{selectionSet: current.SelectionSet, depth: 1})
			}
		case *ast.FragmentDefinition:
			inspector.fragments++
			if inspector.fragments > limits.maxFragments {
				return nil, limitError("graphql_fragments_exceeded", "GraphQL fragment count exceeds the safe limit", inspector.fragments, limits.maxFragments)
			}
			name := ""
			if current.Name != nil {
				name = current.Name.Value
			}
			if _, exists := inspector.fragmentGraph[name]; !exists {
				inspector.fragmentGraph[name] = nil
			}
			if err := inspector.addDirectives(current.Directives); err != nil {
				return nil, err
			}
			if current.SelectionSet != nil {
				inspector.selectionStack = append(inspector.selectionStack, graphQLSelectionInspection{selectionSet: current.SelectionSet, fragmentName: name, depth: 1})
			}
		}
	}

	for len(inspector.selectionStack) != 0 {
		last := len(inspector.selectionStack) - 1
		current := inspector.selectionStack[last]
		inspector.selectionStack = inspector.selectionStack[:last]
		if current.depth > limits.maxSyntaxDepth {
			return nil, limitError("graphql_document_depth_exceeded", "GraphQL document nesting exceeds the safe limit", current.depth, limits.maxSyntaxDepth)
		}
		if err := inspector.addNodes(1 + len(current.selectionSet.Selections)); err != nil {
			return nil, err
		}
		for _, selection := range current.selectionSet.Selections {
			switch selected := selection.(type) {
			case *ast.Field:
				if err := inspector.addArguments(selected.Arguments); err != nil {
					return nil, err
				}
				if err := inspector.addDirectives(selected.Directives); err != nil {
					return nil, err
				}
				if selected.SelectionSet != nil {
					inspector.selectionStack = append(inspector.selectionStack, graphQLSelectionInspection{selectionSet: selected.SelectionSet, fragmentName: current.fragmentName, depth: current.depth + 1})
				}
			case *ast.InlineFragment:
				if err := inspector.addDirectives(selected.Directives); err != nil {
					return nil, err
				}
				if selected.SelectionSet != nil {
					inspector.selectionStack = append(inspector.selectionStack, graphQLSelectionInspection{selectionSet: selected.SelectionSet, fragmentName: current.fragmentName, depth: current.depth + 1})
				}
			case *ast.FragmentSpread:
				if err := inspector.addDirectives(selected.Directives); err != nil {
					return nil, err
				}
				if current.fragmentName != "" && selected.Name != nil {
					inspector.fragmentGraph[current.fragmentName] = append(inspector.fragmentGraph[current.fragmentName], selected.Name.Value)
				}
			}
		}
	}

	for len(inspector.valueStack) != 0 {
		last := len(inspector.valueStack) - 1
		current := inspector.valueStack[last]
		inspector.valueStack = inspector.valueStack[:last]
		if err := inspector.addNodes(1); err != nil {
			return nil, err
		}
		switch value := current.(type) {
		case *ast.ListValue:
			inspector.valueStack = append(inspector.valueStack, value.Values...)
		case *ast.ObjectValue:
			if err := inspector.addNodes(len(value.Fields)); err != nil {
				return nil, err
			}
			for _, field := range value.Fields {
				if field != nil && field.Value != nil {
					inspector.valueStack = append(inspector.valueStack, field.Value)
				}
			}
		}
	}
	return inspector.fragmentGraph, nil
}

func (inspector *graphQLDocumentInspector) addNodes(amount int) error {
	if amount <= 0 {
		return nil
	}
	if amount > inspector.limits.maxNodes || inspector.nodes > inspector.limits.maxNodes-amount {
		return limitError("graphql_document_complexity_exceeded", "GraphQL document exceeds the safe node limit", inspector.limits.maxNodes+1, inspector.limits.maxNodes)
	}
	inspector.nodes += amount
	return nil
}

func (inspector *graphQLDocumentInspector) addArguments(arguments []*ast.Argument) error {
	if err := inspector.addNodes(len(arguments)); err != nil {
		return err
	}
	for _, argument := range arguments {
		if argument != nil && argument.Value != nil {
			inspector.valueStack = append(inspector.valueStack, argument.Value)
		}
	}
	return nil
}

func (inspector *graphQLDocumentInspector) addDirectives(directives []*ast.Directive) error {
	if err := inspector.addNodes(len(directives)); err != nil {
		return err
	}
	for _, directive := range directives {
		if directive != nil {
			if err := inspector.addArguments(directive.Arguments); err != nil {
				return err
			}
		}
	}
	return nil
}

type fragmentGraphFrame struct {
	name       string
	next       int
	childDepth int
}

func validateGraphQLFragmentGraph(graph map[string][]string, maximumDepth int) error {
	state := make(map[string]uint8, len(graph))
	depths := make(map[string]int, len(graph))
	for root := range graph {
		if state[root] != 0 {
			continue
		}
		state[root] = 1
		stack := []fragmentGraphFrame{{name: root}}
		for len(stack) != 0 {
			frame := &stack[len(stack)-1]
			edges := graph[frame.name]
			if frame.next < len(edges) {
				child := edges[frame.next]
				frame.next++
				if _, exists := graph[child]; !exists {
					continue
				}
				switch state[child] {
				case 0:
					if len(stack)+1 > maximumDepth {
						return limitError("graphql_fragment_depth_exceeded", "GraphQL fragment chain exceeds the safe limit", len(stack)+1, maximumDepth)
					}
					state[child] = 1
					stack = append(stack, fragmentGraphFrame{name: child})
				case 1:
					return extendedError{message: "cyclic GraphQL fragment spread", extensions: map[string]interface{}{"code": "graphql_fragment_cycle", "status": 400}}
				case 2:
					if depths[child] > frame.childDepth {
						frame.childDepth = depths[child]
					}
					if 1+frame.childDepth > maximumDepth {
						return limitError("graphql_fragment_depth_exceeded", "GraphQL fragment chain exceeds the safe limit", 1+frame.childDepth, maximumDepth)
					}
				}
				continue
			}

			depth := 1 + frame.childDepth
			if depth > maximumDepth {
				return limitError("graphql_fragment_depth_exceeded", "GraphQL fragment chain exceeds the safe limit", depth, maximumDepth)
			}
			depths[frame.name] = depth
			state[frame.name] = 2
			stack = stack[:len(stack)-1]
			if len(stack) != 0 && depth > stack[len(stack)-1].childDepth {
				stack[len(stack)-1].childDepth = depth
			}
		}
	}
	return nil
}
