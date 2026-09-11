// Package issuetargets exercises aggregate validators through public APIs.
package issuetargets

import (
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func Config() core.Config {
	return core.Config{Name: "Aggregate issue targets", Plugins: []core.Plugin{richtext.New()},
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Collections:  []core.Collection{Collection()},
	}
}

func Collection() core.Collection {
	linkFields := field.Fields{field.Text("url").Label("URL")}
	links := field.Array("links", linkFields)
	sections := field.Array("sections", field.Fields{field.Text("heading"), links}).Validate(validateRows(false))
	card := field.Block{Slug: "card", Fields: field.Fields{field.Text("heading"), links}}
	note := field.Block{Slug: "note", Fields: field.Fields{field.Text("heading"), links}}
	embeddedCard := field.Block{Slug: "card", Fields: field.Fields{
		field.Text("heading"), links.Validate(validateLinks),
	}}
	allow := func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }
	return core.Collection{Slug: "issue-targets", Labels: core.CollectionLabels{Singular: "Issue target", Plural: "Issue targets"},
		Access: core.CollectionAccess{Create: allow, Read: allow, Update: allow, Delete: allow},
		Fields: field.Fields{field.Text("title").Required(), sections,
			sections.Rename("localizedSections").Localized(),
			field.Blocks("content", card, note).Validate(validateRows(true)),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{embeddedCard}}),
			richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{embeddedCard}}).Localized(),
		},
	}
}

func validateRows(blocks bool) field.Validator[store.Value] {
	return func(_ operation.Context, input operation.Value[store.Value]) ([]operation.Issue, error) {
		value, _ := input.Get()
		rows, _ := value.CopyList()
		var issues []operation.Issue
		for _, row := range rows {
			object, _ := row.CopyObject()
			key, _ := object["_key"].StringValue()
			target := operation.At().Row(key)
			if blocks {
				blockType, _ := object["blockType"].StringValue()
				target = operation.At().Block(key, blockType)
			}
			if text, _ := object["heading"].StringValue(); text == "invalid" {
				issues = append(issues, operation.Issue{Code: "heading", Message: "Choose a different heading", Target: target.Field("heading")})
			}
			links, _ := object["links"].CopyList()
			issues = append(issues, linkIssues(target.Field("links"), links)...)
		}
		return issues, nil
	}
}

func validateLinks(_ operation.Context, input operation.Value[store.Value]) ([]operation.Issue, error) {
	value, _ := input.Get()
	links, _ := value.CopyList()
	return linkIssues(operation.At(), links), nil
}

func linkIssues(target operation.IssueTarget, links []store.Value) []operation.Issue {
	var issues []operation.Issue
	for _, link := range links {
		object, _ := link.CopyObject()
		if text, _ := object["url"].StringValue(); text == "invalid" {
			key, _ := object["_key"].StringValue()
			issues = append(issues, operation.Issue{Code: "url", Message: "Choose a different URL", Target: target.Row(key).Field("url")})
		}
	}
	return issues
}
