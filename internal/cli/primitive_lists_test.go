package cli_test

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
	"github.com/riducms/ridu/schema"
)

// Qualify the new kinds and editor value vocabulary through a normally published
// package and a fresh application, including its real production admin compiler.
func TestFreshPrimitiveListProject(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot)
	target := newProjectTarget(t, "primitive-lists")
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := cli.Run(t.Context(), args, &stdout, &stderr, options); code != 0 {
			t.Fatalf("ridu %v: %s\n%s", args, stdout.String(), stderr.String())
		}
	}
	run("new", "--template", "blank", "--database", "sqlite", "--package-manager", "bun", "--module", "example.com/primitive-lists", "--scope", "@fixture", "--no-agent", target)
	write := func(name, content string) {
		t.Helper()
		data := []byte(content)
		if strings.HasSuffix(name, ".go") {
			var err error
			data, err = format.Source(data)
			if err != nil {
				t.Fatalf("format %s: %v", name, err)
			}
		}
		if err := os.WriteFile(filepath.Join(target, filepath.FromSlash(name)), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(target, "content", "config.go")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(config), "[]ridu.Collection{Users}", "[]ridu.Collection{Users, Products}", 1)
	if updated == string(config) {
		t.Fatal("fresh blank collection list not found")
	}
	write("content/config.go", updated)
	write("content/products.go", `package content

import (
    "github.com/riducms/ridu"
    "github.com/riducms/ridu/field"
    "github.com/riducms/ridu/operation"
)

var Products = ridu.Collection{
    Slug: "products",
    Fields: field.Fields{
        field.Text("title").Required(),
        field.TextList("sellingPoints").MinRows(1).MaxRows(8).MaxLength(120).
            Default("Solid oak").Admin(field.Admin{Editor: field.Component("app:points")}),
        field.NumberList("availableSizes").Min(0).MaxRows(20).
            DefaultFrom(func(operation.DefaultContext) (operation.Value[[]float64], error) {
                return operation.Present([]float64{0, 8, 10}), nil
            }),
    },
}
`)
	write("admin/src/admin.config.ts", `import { defineAdmin } from "@riducms/plugin/admin";
import { defineFieldEditor } from "@riducms/plugin/editor";
import { generatedAdminPlugins } from "@/ridu.plugins.generated";
import Points from "@/fields/text-editor.svelte";

export default defineAdmin({
    plugins: generatedAdminPlugins,
    fields: { "app:points": defineFieldEditor({ type: "text-list", component: Points }) },
});
`)
	write("admin/src/fields/text-editor.svelte", `<script lang="ts">
    import type { FieldEditorProps, FieldEditorType, FieldEditorValue } from "@riducms/plugin/editor";
    import Field from "@riducms/plugin/editor/field";
    let { field }: FieldEditorProps<"text-list"> = $props();
    const kind: FieldEditorType = "text-list";
    function add() {
        const next: FieldEditorValue<typeof kind> = [...(field.value ?? []), "Oak"];
        field.set(next);
    }
</script>
<Field {field}>
    <ul>{#each field.value ?? [] as item, index (index)}<li>{item}</li>{/each}</ul>
    <button type="button" disabled={field.readOnly} onclick={add}>Add selling point</button>
</Field>
`)
	run("generate")
	run("generate", "--check")
	run("migrate", "create", "--name", "initial")
	publishFrontendPackages(t, target)
	install := exec.CommandContext(t.Context(), "bun", "install")
	install.Dir = target
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install fresh project dependencies: %v\n%s", err, output)
	}
	// The CLI check dispatch/receipt contract is covered independently. Run the
	// real application's type checker here, then its release build once, instead
	// of repeating Go vet/tests and an admin registration build through check.
	check := exec.CommandContext(t.Context(), "bun", "run", "--cwd", "admin", "check")
	check.Dir = target
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("fresh custom editor type check: %v\n%s", err, output)
	}
	run("build")
	encoded, err := os.ReadFile(filepath.Join(target, "generated", "ridu.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug != "products" {
			continue
		}
		found = true
		if collection.Fields[1].Type != schema.FieldTypeTextList || collection.Fields[1].Admin.Editor.Reference != "app:points" || collection.Fields[2].Type != schema.FieldTypeNumberList || !collection.Fields[2].DynamicDefault {
			t.Fatalf("fresh project lost primitive list contracts: %#v", collection.Fields)
		}
	}
	if !found {
		t.Fatal("fresh product collection missing")
	}
	if _, err := os.Stat(filepath.Join(target, "dist", "primitive-lists")); err != nil {
		t.Fatal(err)
	}
	t.Log("fresh blank project: primitive fields, typed dynamic default, zero-config list editor, deterministic generation, migration, custom editor type check and production build passed")
}
