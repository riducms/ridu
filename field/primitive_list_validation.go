package field

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func (d View) listDefaultIssues(issues *declarationIssues) {
	add := func(message string) { issues.issue("invalid_default", "default", message) }
	var items []json.RawMessage
	if json.Unmarshal([]byte(d.defaultValue.String()), &items) != nil {
		add("list default must be a primitive array")
		return
	}
	if d.required && len(items) == 0 || len(items) < d.minRows || d.maxRows > 0 && len(items) > d.maxRows {
		add("list default violates requiredness or the configured item count")
	}
	for i, item := range items {
		if d.kind == KindTextList {
			var value string
			if json.Unmarshal(item, &value) != nil || string(item) == "null" {
				add(fmt.Sprintf("item %d must be a string", i+1))
				continue
			}
			length := utf8.RuneCountInString(value)
			if d.minLength != nil && length < *d.minLength || d.maxLength != nil && length > *d.maxLength {
				add(fmt.Sprintf("item %d violates the configured text length", i+1))
			}
		} else if d.kind == KindNumberList {
			var value float64
			if json.Unmarshal(item, &value) != nil || string(item) == "null" {
				add(fmt.Sprintf("item %d must be a finite number", i+1))
				continue
			}
			if d.minimum != nil && value < *d.minimum || d.maximum != nil && value > *d.maximum {
				add(fmt.Sprintf("item %d violates the configured numeric bounds", i+1))
			}
		}
	}
}
