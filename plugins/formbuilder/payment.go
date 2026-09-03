package formbuilder

import (
	"fmt"
	"math"
	"strings"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

// PriceCondition describes one conditional arithmetic adjustment.
type PriceCondition struct {
	FieldToUse        string
	Condition         string
	ValueForCondition string
	Operator          string
	ValueType         string
	ValueForOperator  string
}

// PaymentContext is the trusted input supplied before a submission commits.
type PaymentContext struct {
	Context        ridu.HookContext
	Form           store.Document
	Field          store.Values
	SubmissionData map[string]store.Value
	Total          float64
}

// HandlePayment performs application-owned payment work and returns the JSON
// value persisted with the submission. Returning an error aborts the write.
type HandlePayment func(PaymentContext) (store.Value, error)

func (plugin *Plugin) processPayment(context ridu.HookContext, allowedFieldTypes map[FieldType]struct{}) error {
	if context.Operation != ridu.OperationCreate || plugin.config.HandlePayment == nil {
		return nil
	}
	formID, valid := relationshipID(context.Data["form"])
	if !valid {
		return nil
	}
	form, err := context.Local.FindWithOptions(context.Context, string(plugin.config.FormsSlug), formID, ridu.FindOptions{Actor: context.Actor, ActorCollection: context.ActorCollection, Locale: context.Locale})
	if err != nil {
		return err
	}
	definitions, _ := parseFormFields(form.Values, allowedFieldTypes)
	rows, _ := listValue(context.Data, "submissionData")
	values, _, _ := submissionValues(rows)
	for _, definition := range definitions {
		if definition.Type != FieldPayment {
			continue
		}
		if _, submitted := values[definition.Name]; !submitted {
			return nil
		}
		basePrice, _ := numberValue(definition.Data, "basePrice")
		conditions := priceConditions(definition.Data)
		total, totalErr := GetPaymentTotal(basePrice, conditions, values)
		if totalErr != nil {
			return totalErr
		}
		result, paymentErr := plugin.config.HandlePayment(PaymentContext{
			Context: detachedPaymentHookContext(context), Form: store.CloneDocument(form),
			Field: store.CloneValues(definition.Data), SubmissionData: store.CloneValues(values), Total: total,
		})
		if paymentErr != nil {
			return paymentErr
		}
		context.Data["payment"] = result
		return nil
	}
	return nil
}

// GetPaymentTotal applies conditions in order, matching Payload's form-builder
// price model while rejecting non-finite arithmetic and division by zero.
func GetPaymentTotal(basePrice float64, conditions []PriceCondition, values map[string]store.Value) (float64, error) {
	if math.IsNaN(basePrice) || math.IsInf(basePrice, 0) || basePrice < 0 {
		return 0, fmt.Errorf("base price must be finite and non-negative")
	}
	total := basePrice
	for index, condition := range conditions {
		candidate, exists := values[condition.FieldToUse]
		if !exists || !priceConditionMatches(candidate, condition) {
			continue
		}
		operand := 0.0
		if condition.ValueType == "valueOfField" {
			value, exists := values[condition.ValueForOperator]
			if !exists {
				return 0, fmt.Errorf("price condition %d references missing value field %q", index, condition.ValueForOperator)
			}
			var valid bool
			operand, valid = numberFromValue(value)
			if !valid {
				return 0, fmt.Errorf("price condition %d value field %q is not numeric", index, condition.ValueForOperator)
			}
		} else {
			parsed, err := parseFiniteNumber(condition.ValueForOperator)
			if err != nil {
				return 0, fmt.Errorf("price condition %d: %w", index, err)
			}
			operand = parsed
		}
		switch condition.Operator {
		case "add":
			total += operand
		case "subtract":
			total -= operand
		case "multiply":
			total *= operand
		case "divide":
			if operand == 0 {
				return 0, fmt.Errorf("price condition %d divides by zero", index)
			}
			total /= operand
		default:
			return 0, fmt.Errorf("price condition %d has unsupported operator %q", index, condition.Operator)
		}
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return 0, fmt.Errorf("price condition %d produced a non-finite total", index)
		}
		if total < 0 {
			return 0, fmt.Errorf("price condition %d produced a negative total", index)
		}
	}
	return total, nil
}

func detachedPaymentHookContext(context ridu.HookContext) ridu.HookContext {
	detached := context
	detached.Data = store.CloneValues(context.Data)
	if context.Actor != nil {
		actor := store.CloneDocument(*context.Actor)
		detached.Actor = &actor
	}
	if context.Document != nil {
		document := store.CloneDocument(*context.Document)
		detached.Document = &document
	}
	if context.Original != nil {
		original := store.CloneDocument(*context.Original)
		detached.Original = &original
	}
	return detached
}

func priceConditions(values store.Values) []PriceCondition {
	rows, _ := listValue(values, "priceConditions")
	conditions := make([]PriceCondition, 0, len(rows))
	for _, value := range rows {
		row, valid := value.ObjectValue()
		if !valid {
			continue
		}
		fieldToUse, _ := stringValue(row, "fieldToUse")
		condition, _ := stringValue(row, "condition")
		valueForCondition, _ := stringValue(row, "valueForCondition")
		operator, _ := stringValue(row, "operator")
		valueType, _ := stringValue(row, "valueType")
		valueForOperator, _ := stringValue(row, "valueForOperator")
		conditions = append(conditions, PriceCondition{FieldToUse: fieldToUse, Condition: condition, ValueForCondition: valueForCondition, Operator: operator, ValueType: valueType, ValueForOperator: valueForOperator})
	}
	return conditions
}

func priceConditionMatches(value store.Value, condition PriceCondition) bool {
	switch condition.Condition {
	case "hasValue":
		switch value.Kind() {
		case store.ValueNull:
			return false
		case store.ValueString:
			text, _ := value.StringValue()
			return strings.TrimSpace(text) != ""
		case store.ValueList:
			items, _ := value.Values()
			return len(items) != 0
		default:
			return true
		}
	case "equals":
		return scalarString(value) == condition.ValueForCondition
	case "notEquals":
		return scalarString(value) != condition.ValueForCondition
	default:
		return false
	}
}

func scalarString(value store.Value) string {
	if text, valid := value.StringValue(); valid {
		return text
	}
	if number, valid := value.NumberValue(); valid {
		return fmt.Sprintf("%g", number)
	}
	if boolean, valid := value.BooleanValue(); valid {
		if boolean {
			return "true"
		}
		return "false"
	}
	return ""
}

func parseFiniteNumber(value string) (float64, error) {
	parsed, valid := numberFromValue(store.String(value))
	if !valid || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("value %q is not a finite number", value)
	}
	return parsed, nil
}
