package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/jsonlimit"
)

type requestEnvelope struct {
	Query         string          `json:"query"`
	OperationName string          `json:"operationName"`
	Variables     json.RawMessage `json:"variables"`
}

func (executable *executable) serve(endpoint ridu.EndpointContext) {
	writer := endpoint.Writer
	request := endpoint.Request
	responseType := "application/json; charset=utf-8"
	if strings.Contains(request.Header.Get("Accept"), "application/graphql-response+json") {
		responseType = "application/graphql-response+json; charset=utf-8"
	}
	writer.Header().Set("Content-Type", responseType)
	writer.Header().Set("Cache-Control", "private, no-store")
	mediaType, _, mediaError := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaError != nil || mediaType != "application/json" {
		writeGraphQLError(writer, http.StatusUnsupportedMediaType, "GraphQL requests require application/json", "unsupported_media_type")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, executable.options.MaxBodyBytes)
	encoded, readError := io.ReadAll(request.Body)
	if readError != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(readError, &tooLarge) {
			writeGraphQLError(writer, http.StatusRequestEntityTooLarge, "GraphQL request body exceeds the configured limit", "request_too_large")
			return
		}
		writeGraphQLError(writer, http.StatusBadRequest, "invalid GraphQL request body", "bad_request")
		return
	}
	if err := jsonlimit.Validate(encoded, jsonlimit.DefaultMaxDepth, jsonlimit.DefaultMaxTokens); err != nil {
		code, message := "bad_request", "invalid GraphQL request body"
		if errors.Is(err, jsonlimit.ErrTooDeep) || errors.Is(err, jsonlimit.ErrTooManyTokens) {
			code, message = "graphql_json_complexity_exceeded", "GraphQL JSON body exceeds the structural complexity limit"
		}
		writeGraphQLError(writer, http.StatusBadRequest, message, code)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var envelope requestEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		writeGraphQLError(writer, http.StatusBadRequest, "invalid GraphQL request body", "bad_request")
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeGraphQLError(writer, http.StatusBadRequest, "invalid GraphQL request body", "bad_request")
		return
	}
	if strings.TrimSpace(envelope.Query) == "" {
		writeGraphQLError(writer, http.StatusBadRequest, "query is required", "bad_request")
		return
	}
	if len(envelope.Variables) > executable.options.MaxVariableBytes {
		writeGraphQLError(writer, http.StatusRequestEntityTooLarge, "GraphQL variables exceed the configured limit", "variables_too_large")
		return
	}
	variables := map[string]interface{}{}
	if len(envelope.Variables) != 0 && !bytes.Equal(bytes.TrimSpace(envelope.Variables), []byte("null")) {
		if err := json.Unmarshal(envelope.Variables, &variables); err != nil {
			writeGraphQLError(writer, http.StatusBadRequest, "variables must be a JSON object", "bad_request")
			return
		}
	}
	document, err := analyzeOperation(&executable.schema, envelope.Query, envelope.OperationName, variables, executable.options)
	if err != nil {
		writeExtendedGraphQLError(writer, http.StatusOK, err)
		return
	}
	rules := append([]enginegraphql.ValidationRuleFn(nil), enginegraphql.SpecifiedRules...)
	rules = append(rules, executable.options.ValidationRules...)
	validation := enginegraphql.ValidateDocument(&executable.schema, document, rules)
	if !validation.IsValid {
		writeGraphQLResult(endpoint, http.StatusOK, &enginegraphql.Result{Data: nil, Errors: validation.Errors})
		return
	}
	ctx := context.WithValue(request.Context(), requestStateKey{}, requestState{
		rawVariables: variables, actor: endpoint.Actor, actorCollection: endpoint.ActorCollection, token: requestToken(request),
		clientIP: endpoint.ClientIP, userAgent: request.UserAgent(), admitAuth: endpoint.AdmitAuthAttempt,
	})
	result := enginegraphql.Execute(enginegraphql.ExecuteParams{
		Schema: executable.schema, AST: document, OperationName: envelope.OperationName,
		Args: variables, Context: ctx,
	})
	sanitizeExecutionErrors(endpoint, result.Errors)
	writeGraphQLResult(endpoint, http.StatusOK, result)
}

func sanitizeExecutionErrors(endpoint ridu.EndpointContext, failures []gqlerrors.FormattedError) {
	for index := range failures {
		failure := &failures[index]
		cause, public := graphQLExecutionError(failure.OriginalError())
		if public {
			if endpoint.ReportError != nil {
				endpoint.ReportError(nil, graphQLErrorCode(*failure, "graphql_error"))
			}
			continue
		}
		if cause == nil {
			cause = errors.New("GraphQL execution failed")
		}
		if endpoint.ReportError != nil {
			endpoint.ReportError(cause, "internal_error")
		}
		failure.Message = "internal server error"
		failure.Extensions = map[string]interface{}{"code": "internal_error", "status": http.StatusInternalServerError}
	}
}

func graphQLExecutionError(err error) (error, bool) {
	for {
		switch wrapped := err.(type) {
		case *gqlerrors.Error:
			if wrapped.OriginalError == nil {
				return wrapped, false
			}
			err = wrapped.OriginalError
		case gqlerrors.Error:
			if wrapped.OriginalError == nil {
				return wrapped, false
			}
			err = wrapped.OriginalError
		default:
			switch failure := err.(type) {
			case transportFailure:
				return failure.cause, graphQLStatus(failure.extensions) < http.StatusInternalServerError
			case *transportFailure:
				return failure.cause, graphQLStatus(failure.extensions) < http.StatusInternalServerError
			case extendedError, *extendedError:
				return err, true
			}
			var trusted interface{ TrustedCause() error }
			if errors.As(err, &trusted) {
				return trusted.TrustedCause(), false
			}
			return err, false
		}
	}
}

func graphQLStatus(extensions map[string]interface{}) int {
	switch encoded := extensions["status"].(type) {
	case int:
		return encoded
	case float64:
		return int(encoded)
	}
	return http.StatusInternalServerError
}

func graphQLErrorCode(failure gqlerrors.FormattedError, fallback string) string {
	if encoded, ok := failure.Extensions["code"].(string); ok && encoded != "" {
		return encoded
	}
	return fallback
}

func writeGraphQLResult(endpoint ridu.EndpointContext, status int, result *enginegraphql.Result) {
	encoded, err := json.Marshal(result)
	if err != nil {
		if endpoint.ReportError != nil {
			endpoint.ReportError(fmt.Errorf("marshal GraphQL response: %w", err), "internal_error")
		}
		writeGraphQLError(endpoint.Writer, http.StatusInternalServerError, "internal server error", "internal_error")
		return
	}
	endpoint.Writer.WriteHeader(status)
	_, _ = endpoint.Writer.Write(append(encoded, '\n'))
}

func requestToken(request *http.Request) string {
	authorization := strings.Fields(request.Header.Get("Authorization"))
	if len(authorization) == 2 {
		for _, scheme := range []string{"Bearer", "Session"} {
			if strings.EqualFold(authorization[0], scheme) {
				return authorization[1]
			}
		}
	}
	if cookie, err := request.Cookie("ridu_session"); err == nil {
		return cookie.Value
	}
	return ""
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeGraphQLError(writer http.ResponseWriter, status int, message, code string) {
	writeExtendedGraphQLError(writer, status, extendedError{message: message, extensions: map[string]interface{}{"code": code, "status": status}})
}

func writeExtendedGraphQLError(writer http.ResponseWriter, status int, err error) {
	formatted := gqlerrors.FormatError(err)
	if extended, ok := err.(interface{ Extensions() map[string]interface{} }); ok {
		formatted.Extensions = extended.Extensions()
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(&enginegraphql.Result{Data: nil, Errors: []gqlerrors.FormattedError{formatted}})
}

type operationAnalysis struct {
	options             Options
	limits              graphQLDocumentLimits
	schema              *enginegraphql.Schema
	variables           map[string]interface{}
	fragments           map[string]*ast.FragmentDefinition
	rootCosts           map[string]int
	mutation            bool
	aliases             int
	depth               int
	complexity          int
	fragmentExpansions  int
	activeFragmentNames map[string]bool
}

func analyzeOperation(schema *enginegraphql.Schema, queryText, operationName string, variables map[string]interface{}, options Options) (*ast.Document, error) {
	limits := documentLimitsFor(options)
	if err := guardGraphQLSource(queryText, limits); err != nil {
		return nil, err
	}
	document, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(queryText), Name: "GraphQL request"})})
	if err != nil {
		return nil, extendedError{message: "GraphQL syntax is invalid", extensions: map[string]interface{}{"code": "graphql_parse_failed", "status": 400}}
	}
	fragmentGraph, err := inspectGraphQLDocument(document, limits)
	if err != nil {
		return nil, err
	}
	if err := validateGraphQLFragmentGraph(fragmentGraph, limits.maxFragmentDepth); err != nil {
		return nil, err
	}
	rootCosts := make(map[string]int, len(options.Queries)+len(options.Mutations))
	for _, field := range options.Queries {
		if field.Cost > 1 {
			rootCosts[field.Name] = field.Cost
		}
	}
	for _, field := range options.Mutations {
		if field.Cost > 10 {
			rootCosts[field.Name] = field.Cost
		}
	}
	analysis := &operationAnalysis{
		options: options, limits: limits, schema: schema, variables: variables,
		fragments: make(map[string]*ast.FragmentDefinition), rootCosts: rootCosts,
		activeFragmentNames: make(map[string]bool),
	}
	var operations []*ast.OperationDefinition
	for _, definition := range document.Definitions {
		switch current := definition.(type) {
		case *ast.OperationDefinition:
			operations = append(operations, current)
		case *ast.FragmentDefinition:
			analysis.fragments[current.Name.Value] = current
		}
	}
	var selected *ast.OperationDefinition
	for _, operation := range operations {
		name := ""
		if operation.Name != nil {
			name = operation.Name.Value
		}
		if operationName != "" && name == operationName {
			selected = operation
		}
	}
	if operationName == "" && len(operations) == 1 {
		selected = operations[0]
	}
	if selected == nil {
		return nil, extendedError{message: "exactly one GraphQL operation must be selected", extensions: map[string]interface{}{"code": "graphql_operation_required", "status": 400}}
	}
	analysis.mutation = selected.Operation == "mutation"
	var rootType enginegraphql.Composite = schema.QueryType()
	if analysis.mutation {
		rootType = schema.MutationType()
	}
	if err := analysis.walk(selected.SelectionSet, rootType); err != nil {
		return nil, err
	}
	if analysis.depth > options.MaxDepth {
		return nil, limitError("graphql_depth_exceeded", "GraphQL selection depth exceeds the configured limit", analysis.depth, options.MaxDepth)
	}
	if analysis.aliases > options.MaxAliases {
		return nil, limitError("graphql_aliases_exceeded", "GraphQL alias count exceeds the configured limit", analysis.aliases, options.MaxAliases)
	}
	if analysis.complexity > options.MaxComplexity {
		return nil, limitError("graphql_complexity_exceeded", "GraphQL complexity exceeds the configured limit", analysis.complexity, options.MaxComplexity)
	}
	return document, nil
}

type operationWalkActionKind uint8

const (
	operationWalkSelections operationWalkActionKind = iota
	operationWalkFieldExit
	operationWalkFragmentExit
)

type operationWalkAction struct {
	kind         operationWalkActionKind
	selectionSet *ast.SelectionSet
	index        int
	depth        int
	root         bool
	parent       enginegraphql.Composite
	before       int
	fieldCost    int
	multiplier   int
	fragmentName string
}

// walk is iterative so selected field nesting and fragment chains cannot use
// the Go call stack. The pre-parser and document inspection have already
// bounded the AST, while this pass separately bounds actual fragment
// expansions (including repeated spreads through a DAG).
func (analysis *operationAnalysis) walk(selectionSet *ast.SelectionSet, parent enginegraphql.Composite) error {
	if selectionSet == nil {
		return nil
	}
	stack := []operationWalkAction{{kind: operationWalkSelections, selectionSet: selectionSet, depth: 1, root: true, parent: parent}}
	for len(stack) != 0 {
		last := len(stack) - 1
		action := stack[last]
		stack = stack[:last]
		switch action.kind {
		case operationWalkFieldExit:
			childCost := analysis.complexity - action.before - action.fieldCost
			if childCost > 0 && action.multiplier > 1 {
				if err := analysis.addComplexityProduct(childCost, action.multiplier-1); err != nil {
					return err
				}
			}
			continue
		case operationWalkFragmentExit:
			delete(analysis.activeFragmentNames, action.fragmentName)
			continue
		}

		if action.selectionSet == nil || action.index >= len(action.selectionSet.Selections) {
			continue
		}
		if action.depth > analysis.options.MaxDepth {
			return limitError("graphql_depth_exceeded", "GraphQL selection depth exceeds the configured limit", action.depth, analysis.options.MaxDepth)
		}
		if action.depth > analysis.depth {
			analysis.depth = action.depth
		}
		selection := action.selectionSet.Selections[action.index]
		action.index++
		stack = append(stack, action)
		switch current := selection.(type) {
		case *ast.Field:
			if !analysis.options.AllowIntrospection && current.Name != nil && (current.Name.Value == "__schema" || current.Name.Value == "__type") {
				return extendedError{message: "GraphQL introspection is disabled", extensions: map[string]interface{}{"code": "graphql_introspection_disabled", "status": 400}}
			}
			if current.Alias != nil && current.Name != nil && current.Alias.Value != current.Name.Value {
				analysis.aliases++
				if analysis.aliases > analysis.options.MaxAliases {
					return limitError("graphql_aliases_exceeded", "GraphQL alias count exceeds the configured limit", analysis.aliases, analysis.options.MaxAliases)
				}
			}
			before := analysis.complexity
			fieldCost := 1
			if action.root && analysis.mutation {
				fieldCost = 10
			}
			fieldName := ""
			if current.Name != nil {
				fieldName = current.Name.Value
			}
			if action.root && analysis.rootCosts[fieldName] > fieldCost {
				fieldCost = analysis.rootCosts[fieldName]
			}
			if err := analysis.addComplexity(fieldCost); err != nil {
				return err
			}
			definition := compositeField(action.parent, fieldName)
			if current.SelectionSet == nil {
				continue
			}
			multiplier := 1
			if fieldHasArgument(definition, "limit") {
				multiplier = requestedLimit(current, analysis.variables, analysis.options.MaxListLimit)
			}
			if multiplier > 1 {
				stack = append(stack, operationWalkAction{kind: operationWalkFieldExit, before: before, fieldCost: fieldCost, multiplier: multiplier})
			}
			stack = append(stack, operationWalkAction{
				kind: operationWalkSelections, selectionSet: current.SelectionSet,
				depth: action.depth + 1, parent: outputComposite(definition),
			})
		case *ast.InlineFragment:
			fragmentParent := action.parent
			if current.TypeCondition != nil && current.TypeCondition.Name != nil {
				fragmentParent = analysis.compositeType(current.TypeCondition.Name.Value, action.parent)
			}
			stack = append(stack, operationWalkAction{
				kind: operationWalkSelections, selectionSet: current.SelectionSet,
				depth: action.depth, root: action.root, parent: fragmentParent,
			})
		case *ast.FragmentSpread:
			name := ""
			if current.Name != nil {
				name = current.Name.Value
			}
			if analysis.activeFragmentNames[name] {
				return extendedError{message: "cyclic GraphQL fragment spread", extensions: map[string]interface{}{"code": "graphql_fragment_cycle", "status": 400}}
			}
			fragment := analysis.fragments[name]
			if fragment == nil {
				continue
			}
			analysis.fragmentExpansions++
			if analysis.fragmentExpansions > analysis.limits.maxFragmentExpansions {
				return limitError("graphql_fragment_expansions_exceeded", "GraphQL fragment expansions exceed the safe limit", analysis.fragmentExpansions, analysis.limits.maxFragmentExpansions)
			}
			analysis.activeFragmentNames[name] = true
			stack = append(stack, operationWalkAction{kind: operationWalkFragmentExit, fragmentName: name})
			fragmentParent := action.parent
			if fragment.TypeCondition != nil && fragment.TypeCondition.Name != nil {
				fragmentParent = analysis.compositeType(fragment.TypeCondition.Name.Value, action.parent)
			}
			stack = append(stack, operationWalkAction{
				kind: operationWalkSelections, selectionSet: fragment.SelectionSet,
				depth: action.depth, root: action.root, parent: fragmentParent,
			})
		}
	}
	return nil
}

func (analysis *operationAnalysis) addComplexity(amount int) error {
	if amount <= 0 {
		return nil
	}
	if amount > analysis.options.MaxComplexity || analysis.complexity > analysis.options.MaxComplexity-amount {
		return limitError("graphql_complexity_exceeded", "GraphQL complexity exceeds the configured limit", analysis.options.MaxComplexity+1, analysis.options.MaxComplexity)
	}
	analysis.complexity += amount
	return nil
}

func (analysis *operationAnalysis) addComplexityProduct(value, multiplier int) error {
	if value <= 0 || multiplier <= 0 {
		return nil
	}
	remaining := analysis.options.MaxComplexity - analysis.complexity
	if remaining < 0 || value > remaining/multiplier {
		return limitError("graphql_complexity_exceeded", "GraphQL complexity exceeds the configured limit", analysis.options.MaxComplexity+1, analysis.options.MaxComplexity)
	}
	analysis.complexity += value * multiplier
	return nil
}

func compositeField(parent enginegraphql.Composite, fieldName string) *enginegraphql.FieldDefinition {
	switch current := parent.(type) {
	case *enginegraphql.Object:
		return current.Fields()[fieldName]
	case *enginegraphql.Interface:
		return current.Fields()[fieldName]
	default:
		return nil
	}
}

func outputComposite(field *enginegraphql.FieldDefinition) enginegraphql.Composite {
	if field == nil {
		return nil
	}
	named := enginegraphql.GetNamed(field.Type)
	composite, _ := named.(enginegraphql.Composite)
	return composite
}

func (analysis *operationAnalysis) compositeType(name string, fallback enginegraphql.Composite) enginegraphql.Composite {
	if analysis.schema == nil {
		return fallback
	}
	composite, ok := analysis.schema.Type(name).(enginegraphql.Composite)
	if !ok {
		return fallback
	}
	return composite
}

func fieldHasArgument(field *enginegraphql.FieldDefinition, argumentName string) bool {
	if field == nil {
		return false
	}
	for _, argument := range field.Args {
		if argument.Name() == argumentName {
			return true
		}
	}
	return false
}

func requestedLimit(field *ast.Field, variables map[string]interface{}, maximum int) int {
	for _, argument := range field.Arguments {
		if argument.Name.Value != "limit" {
			continue
		}
		switch value := argument.Value.(type) {
		case *ast.IntValue:
			parsed, _ := strconv.Atoi(value.Value)
			if parsed > 0 && parsed < maximum {
				return parsed
			}
		case *ast.Variable:
			if parsed, ok := variables[value.Name.Value].(float64); ok && parsed > 0 && int(parsed) < maximum {
				return int(parsed)
			}
			if parsed, ok := variables[value.Name.Value].(int); ok && parsed > 0 && parsed < maximum {
				return parsed
			}
		}
		return maximum
	}
	if maximum < 10 {
		return maximum
	}
	return 10
}

func limitError(code, message string, actual, maximum int) error {
	return extendedError{message: message, extensions: map[string]interface{}{"code": code, "status": 400, "actual": actual, "maximum": maximum}}
}
