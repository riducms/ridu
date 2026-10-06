package typescript

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
)

// The per-definition path contracts must describe exactly the vocabulary that
// enumerating every placement path describes. The oracle below enumerates the
// resolved placements directly, as the generator did before definitions were
// composed, and is compared with the expanded generated contracts.
func TestPathContractsExpandToEveryPlacementPath(t *testing.T) {
	for name, config := range map[string]ridu.Config{"references": pathFixture(true), "inline": pathFixture(false), "layered": layeredFixture(4, 3)} {
		t.Run(name, func(t *testing.T) {
			manifest, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := manifest.Snapshot()
			catalog, err := blocktypes.Build(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			names := map[schema.StableID]string{}
			for _, resource := range append(append([]schema.Collection{}, snapshot.Collections...), snapshot.Globals...) {
				names[resource.ID] = pascal(string(resource.ID))
			}
			generator := &clientGenerator{blocks: catalog, draftReadFields: catalog.DraftReadFields(snapshot), collectionNames: names, pluginTypes: pluginFieldTypes(snapshot.Plugins)}
			contracts := newPathContracts(generator)
			validation := newValidationPaths(contracts)
			checked := 0
			for _, resource := range append(append([]schema.Collection{}, snapshot.Collections...), snapshot.Globals...) {
				fields := storedFields(resource.Fields)

				// Paths come from the placement walk; types come from each placement's
				// definition field, because the catalog indexes registered blocks'
				// fields by their definition-relative IDs.
				definitions := map[string]schema.Field{}
				schema.WalkPlacements(fields, func(segments []string, entry schema.Field, _ bool) bool {
					definitions[strings.Join(segments, ".")] = entry
					return true
				})
				expected := map[string]string{}
				for _, candidate := range placementWhereFields(fields, "") {
					entry, found := definitions[candidate.path]
					if !found {
						entry = candidate.field
					}
					expected[candidate.path] = generator.whereType(entry)
				}
				actual := map[string]string{}
				expandPaths(contracts.whereDefinition(fields), "", contracts.where, func(path string, entry schema.Field) { actual[path] = generator.whereType(entry) })
				comparePaths(t, string(resource.ID)+" where", expected, actual)

				expected = map[string]string{}
				for _, candidate := range placementRelationshipFields(fields) {
					expected[candidate.Path.String()] = generator.populateType(candidate)
				}
				actual = map[string]string{}
				expandPaths(contracts.populateDefinition(fields), "", contracts.populate, func(path string, entry schema.Field) { actual[path] = generator.populateType(entry) })
				comparePaths(t, string(resource.ID)+" populate", expected, actual)

				expected = map[string]string{}
				for _, path := range placementValidationPaths(writableFields(resource.Fields)) {
					expected[path] = ""
				}
				actual = map[string]string{}
				for _, term := range validation.terms(writableFields(resource.Fields), false) {
					for _, path := range expandValidationTerm(t, term, validation.unions) {
						actual[path] = ""
					}
				}
				comparePaths(t, string(resource.ID)+" validation", expected, actual)
				checked += len(expected)
			}
			if contracts.err != nil {
				t.Fatal(contracts.err)
			}
			if checked == 0 {
				t.Fatal("fixture produced no paths")
			}
		})
	}
}

func comparePaths(t *testing.T, label string, expected, actual map[string]string) {
	t.Helper()
	for path, value := range expected {
		if got, ok := actual[path]; !ok {
			t.Errorf("%s: generated contracts omit %q", label, path)
		} else if got != value {
			t.Errorf("%s: %q is %s, want %s", label, path, got, value)
		}
	}
	for path := range actual {
		if _, ok := expected[path]; !ok {
			t.Errorf("%s: generated contracts add %q", label, path)
		}
	}
}

func expandPaths(definition pathDefinition, prefix string, variants map[string]pathDefinition, visit func(string, schema.Field)) {
	for _, entry := range definition.entries {
		visit(prefix+entry.path, entry.field)
	}
	for _, placement := range definition.placements {
		expandPaths(variants[placement.variant], prefix+placement.prefix, variants, visit)
	}
}

// expandValidationTerm substitutes referenced variant vocabularies into one
// rendered union member and returns raw paths.
func expandValidationTerm(t *testing.T, term string, unions map[string][]string) []string {
	t.Helper()
	var raw string
	if strings.HasPrefix(term, "`") {
		raw = strings.Trim(term, "`")
	} else {
		unquoted, err := strconv.Unquote(term)
		if err != nil {
			t.Fatalf("validation term %s: %v", term, err)
		}
		raw = unquoted
	}
	for name, union := range unions {
		reference := "${" + name + "}"
		if !strings.HasSuffix(raw, reference) {
			continue
		}
		var result []string
		for _, member := range union {
			for _, path := range expandValidationTerm(t, member, unions) {
				result = append(result, strings.TrimSuffix(raw, reference)+path)
			}
		}
		return result
	}
	if strings.Contains(raw, "ValidationPath}") {
		t.Fatalf("validation term %s refers to an unemitted vocabulary", term)
	}
	return []string{raw}
}

type placementWhereField struct {
	path  string
	field schema.Field
}

func placementWhereFields(fields []schema.Field, prefix string) []placementWhereField {
	var result []placementWhereField
	for _, candidate := range fields {
		if candidate.Category == schema.FieldCategoryPresentation || candidate.QueryRestricted {
			continue
		}
		path := candidate.Path.String()
		if path == "" {
			path = candidate.Name
			if prefix != "" {
				path = prefix + "." + candidate.Name
			}
		}
		result = append(result, placementWhereField{path: path, field: candidate})
		switch candidate.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if candidate.Nested != nil {
				result = append(result, placementWhereFields(candidate.Nested.ResolvedFields(), path)...)
			}
		case schema.FieldTypeBlocks:
			if candidate.Blocks != nil {
				for _, block := range candidate.Blocks.ResolvedTypes() {
					result = append(result, placementWhereFields(block.ResolvedFields(), path+"."+block.Slug)...)
				}
			}
		}
	}
	return result
}

func placementRelationshipFields(fields []schema.Field) []schema.Field {
	var result []schema.Field
	for _, candidate := range fields {
		if candidate.Type == schema.FieldTypeRelationship || candidate.Type == schema.FieldTypeUpload {
			result = append(result, candidate)
			continue
		}
		result = append(result, placementRelationshipFields(schema.ChildFields(candidate))...)
	}
	return result
}

func placementValidationPaths(fields []schema.Field) []string {
	var paths []string
	var add func(schema.Field, string)
	add = func(candidate schema.Field, prefix string) {
		path := candidate.Name
		if prefix != "" {
			path = prefix + "." + candidate.Name
		}
		paths = append(paths, path)
		variants := []string{path}
		if candidate.Localized {
			variants = append(variants, path+".${Locale}")
			paths = append(paths, path+".${Locale}")
		}
		for _, variant := range variants {
			switch candidate.Type {
			case schema.FieldTypeGroup:
				if candidate.Nested != nil {
					for _, child := range writableFields(candidate.Nested.ResolvedFields()) {
						add(child, variant)
					}
				}
			case schema.FieldTypeArray:
				row := variant + ".${number}"
				paths = append(paths, row, row+"._key")
				if candidate.Nested != nil {
					for _, child := range writableFields(candidate.Nested.ResolvedFields()) {
						add(child, row)
					}
				}
			case schema.FieldTypeBlocks:
				row := variant + ".${number}"
				paths = append(paths, row, row+"._key", row+".blockType")
				if candidate.Blocks != nil {
					for _, block := range candidate.Blocks.ResolvedTypes() {
						for _, child := range writableFields(block.ResolvedFields()) {
							add(child, row)
						}
					}
				}
			case schema.FieldTypePlugin:
				if candidate.Plugin != nil && len(candidate.Plugin.EmbeddedTrees) > 0 {
					paths = append(paths, variant+".${string}")
				}
			case schema.FieldTypeSelect:
				if candidate.Select != nil && candidate.Select.HasMany {
					paths = append(paths, variant+".${number}")
				}
			case schema.FieldTypeRelationship:
				if candidate.Relationship != nil && candidate.Relationship.HasMany {
					paths = append(paths, variant+".${number}")
				}
			case schema.FieldTypeUpload:
				if candidate.Upload != nil && candidate.Upload.HasMany {
					paths = append(paths, variant+".${number}")
				}
			}
		}
	}
	for _, candidate := range fields {
		add(candidate, "")
	}
	return paths
}

// pathFixture exercises every path-bearing field family: localized leaves and
// containers (including blocks under localized ancestors), read-restricted
// and presentation fields, groups and arrays inside blocks, has-many values,
// relationships at every depth, and rich text with embedded blocks.
func pathFixture(references bool) ridu.Config {
	deny := func(operation.Context) (bool, error) { return false, nil }
	defined := map[string]field.Block{}
	// Inline variants share one definition per slug through an explicit TypeName;
	// registered blocks are selected by slug.
	define := func(slug, typeName string, fields field.Fields) {
		block := field.Block{Slug: slug, Fields: fields}
		if !references {
			block.TypeName = typeName
		}
		defined[slug] = block
	}
	blocks := func(name string, slugs ...string) field.BlocksField {
		if references {
			return field.Blocks(name).References(slugs...)
		}
		types := make([]field.Block, len(slugs))
		for index, slug := range slugs {
			types[index] = defined[slug]
		}
		return field.Blocks(name, types...)
	}
	editor := func(name string, slugs ...string) field.PluginField {
		if references {
			return richtext.Field(name, richtext.Config{BlockReferences: slugs})
		}
		types := make([]field.Block, len(slugs))
		for index, slug := range slugs {
			types[index] = defined[slug]
		}
		return richtext.Field(name, richtext.Config{Blocks: types})
	}
	define("note", "Note", field.Fields{
		field.Text("body").Localized(),
		field.Relationship("author", "people"),
		field.Text("secret").Access(field.Access{Read: deny}),
	})
	define("callout", "Callout", field.Fields{
		field.Text("title").Required(),
		field.Upload("image", "assets"),
		editor("detail", "note"),
	})
	define("card", "Card", field.Fields{
		field.Text("heading").Localized(),
		field.MultiSelect("tags", "news", "events"),
		field.Relationships("links", "people"),
		field.Group("style", field.Fields{field.Select("tone", "light", "dark"), blocks("badges", "note")}),
		field.Array("items", field.Fields{field.Text("label").Required(), blocks("notes", "note")}),
		blocks("children", "note"),
		field.UI("guide"),
	})
	define("section", "Section", field.Fields{
		field.Text("anchor"),
		field.Group("restricted", field.Fields{field.Text("hidden")}).Access(field.Access{Read: deny}),
		blocks("content", "card", "note"),
		blocks("translated", "card").Localized(),
		field.Group("localized", field.Fields{blocks("rows", "card")}).Localized(),
	})
	config := ridu.Config{
		Name:    "Path fixture",
		Plugins: []ridu.Plugin{richtext.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: field.Fields{field.Text("name")}},
			{Slug: "assets", Upload: true, Fields: field.Fields{field.Text("alt")}},
			{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{
				field.Text("title").Required().Localized(),
				blocks("layout", "section", "card", "callout"),
				editor("body", "callout"),
				field.Group("seo", field.Fields{field.Text("description"), blocks("extras", "note")}).Localized(),
			}},
		},
		Globals: []ridu.Global{{Slug: "footer", Fields: field.Fields{blocks("layout", "card", "note")}}},
	}
	if references {
		config.Blocks = []field.Block{defined["note"], defined["callout"], defined["card"], defined["section"]}
	}
	return config
}

// layeredFixture builds a reference-block DAG whose containers accept every
// block of all lower layers, so placement paths grow exponentially with depth.
func layeredFixture(leaves, layers int) ridu.Config {
	var registry []field.Block
	var lower []string
	for index := range leaves {
		slug := fmt.Sprintf("leaf-%d", index)
		registry = append(registry, field.Block{Slug: slug, Fields: field.Fields{field.Text("text").Localized(), field.Relationship("target", "pages"), field.Array("items", field.Fields{field.Number("value")})}})
		lower = append(lower, slug)
	}
	for layer := range layers {
		slug := fmt.Sprintf("layer-%d", layer)
		registry = append(registry, field.Block{Slug: slug, Fields: field.Fields{field.Text("anchor"), field.Blocks("content").References(slices.Clone(lower)...)}})
		lower = append(lower, slug)
	}
	return ridu.Config{
		Name:   "Layered blocks",
		Blocks: registry,
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Text("title"),
			field.Blocks("layout").References(lower...),
			field.Blocks("translated").References(lower...).Localized(),
		}}},
	}
}

func TestPathContractsAreProportionalToDefinitions(t *testing.T) {
	sizes := map[int]int{}
	for _, layers := range []int{3, 4, 5} {
		manifest, err := ridu.Resolve(layeredFixture(4, layers))
		if err != nil {
			t.Fatal(err)
		}
		generated, err := Client(manifest)
		if err != nil {
			t.Fatal(err)
		}
		sizes[layers] = len(generated)
		// Every placement path is reachable, but none is spelled out beyond its definition.
		if strings.Contains(string(generated), `"layout.layer-0.content.`) {
			t.Fatalf("generated contracts flatten nested block paths:\n%s", generated)
		}
	}
	// Placement paths multiply with each layer; definitions grow by one block.
	first, second := sizes[4]-sizes[3], sizes[5]-sizes[4]
	if second > first*2 {
		t.Fatalf("generated client grows faster than its definitions: %v", sizes)
	}
}
