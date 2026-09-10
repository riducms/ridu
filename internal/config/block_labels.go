package config

import (
	"strings"
	"unicode"

	pluralize "github.com/gertd/go-pluralize"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// This private rule set is initialized once and never extended at runtime.
// The Go port uses the pluralize English rules used by Payload's formatLabels.
var blockLabelInflector = pluralize.NewClient()

func defaultBlockLabels(slug string) schema.BlockLabels {
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || unicode.IsSpace(r) })
	for i, word := range words {
		letters := []rune(word)
		letters[0] = unicode.ToUpper(letters[0])
		words[i] = string(letters)
	}
	label := strings.Join(words, " ")
	if blockLabelInflector.IsPlural(slug) {
		return schema.BlockLabels{Singular: blockLabelInflector.Singular(label), Plural: label}
	}
	return schema.BlockLabels{Singular: label, Plural: blockLabelInflector.Plural(label)}
}

func (r *resolver) resolveBlockLabels(slug string, authored field.BlockLabels, path string) schema.BlockLabels {
	labels := defaultBlockLabels(slug)
	if singular := strings.TrimSpace(authored.Singular); singular != "" {
		labels.Singular = singular
	}
	if plural := strings.TrimSpace(authored.Plural); plural != "" {
		labels.Plural = plural
	}
	labels.SingularTranslations = r.resolveTranslations(authored.SingularTranslations, path+".singularTranslations")
	labels.PluralTranslations = r.resolveTranslations(authored.PluralTranslations, path+".pluralTranslations")
	return labels
}
