// Package pluginscaffold renders a distributable paired Ridu plugin.
package pluginscaffold

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

var (
	keyPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	modulePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+/\-]*$`)
	adminPattern    = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
	semanticVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
)

// Options defines one independently publishable backend/admin plugin pair.
type Options struct {
	Target           string
	Key              string
	ModulePath       string
	AdminPackage     string
	FrameworkVersion string
}

type data struct {
	Key          string
	GoName       string
	ModulePath   string
	AdminPackage string
	GoVersion    string
	NPMVersion   string
	RiduMinimum  string
	RiduMaximum  string
}

// Create atomically creates a complete plugin-author starter with backend,
// admin package, and both conformance fixtures.
func Create(options Options) (string, error) {
	target, values, err := validate(options)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("target %s already exists", target)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create target parent: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(target), ".ridu-plugin-new-*")
	if err != nil {
		return "", fmt.Errorf("create plugin staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	for path, source := range templates {
		parsed, err := template.New(path).Option("missingkey=error").Parse(source)
		if err != nil {
			return "", fmt.Errorf("parse plugin template %s: %w", path, err)
		}
		fullPath := filepath.Join(staging, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return "", fmt.Errorf("create plugin directory: %w", err)
		}
		var rendered bytes.Buffer
		if err := parsed.Execute(&rendered, values); err != nil {
			return "", fmt.Errorf("render plugin file %s: %w", path, err)
		}
		contents := rendered.Bytes()
		if strings.HasSuffix(path, ".go") {
			contents, err = format.Source(contents)
			if err != nil {
				return "", fmt.Errorf("format plugin file %s: %w", path, err)
			}
		}
		handle, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("create plugin file %s: %w", path, err)
		}
		if _, err := handle.Write(contents); err != nil {
			handle.Close()
			return "", fmt.Errorf("write plugin file %s: %w", path, err)
		}
		if err := handle.Close(); err != nil {
			return "", fmt.Errorf("close plugin file %s: %w", path, err)
		}
	}
	if err := os.Rename(staging, target); err != nil {
		return "", fmt.Errorf("install plugin at %s: %w", target, err)
	}
	return target, nil
}

func validate(options Options) (string, data, error) {
	target, err := filepath.Abs(options.Target)
	if err != nil {
		return "", data{}, fmt.Errorf("resolve plugin target: %w", err)
	}
	if !keyPattern.MatchString(options.Key) {
		return "", data{}, fmt.Errorf("plugin key %q must be lowercase kebab-case", options.Key)
	}
	if !modulePattern.MatchString(options.ModulePath) || !strings.Contains(options.ModulePath, "/") {
		return "", data{}, fmt.Errorf("Go module path %q is invalid", options.ModulePath)
	}
	if !adminPattern.MatchString(options.AdminPackage) {
		return "", data{}, fmt.Errorf("admin package %q is invalid", options.AdminPackage)
	}
	if !semanticVersion.MatchString(options.FrameworkVersion) {
		return "", data{}, fmt.Errorf("framework version %q is not semantic", options.FrameworkVersion)
	}
	npmVersion := strings.TrimPrefix(options.FrameworkVersion, "v")
	coreVersion := strings.SplitN(npmVersion, "-", 2)[0]
	parts := strings.Split(coreVersion, ".")
	minor, _ := strconv.Atoi(parts[1])
	riduMaximum := fmt.Sprintf("%s.%d.0", parts[0], minor+1)
	goName := strings.ReplaceAll(options.Key, "-", "")
	return target, data{Key: options.Key, GoName: goName, ModulePath: options.ModulePath, AdminPackage: options.AdminPackage, GoVersion: "v" + npmVersion, NPMVersion: npmVersion, RiduMinimum: npmVersion, RiduMaximum: riduMaximum}, nil
}

var templates = map[string]string{
	"go.mod": `module {{.ModulePath}}

go 1.25.13

require github.com/riducms/ridu {{.GoVersion}}
`,
	"plugin.go": `package {{.GoName}}

import (
	"encoding/json"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

const Key = "{{.Key}}"

type Value struct {
	Value string ` + "`json:\"value\"`" + `
}

type plugin struct{}

func New() ridu.Plugin { return plugin{} }
func (plugin) Key() string { return Key }

func (plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: "0.1.0",
		GoPackage: "{{.ModulePath}}",
		APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{Minimum: "{{.RiduMinimum}}", MaximumExclusive: "{{.RiduMaximum}}"},
		Admin: &ridu.AdminPluginMetadata{Package: "{{.AdminPackage}}", Export: "adminPlugin", APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1},
		FieldTypes: []ridu.PluginFieldType{
		{
			Key: Key,
			TypeScriptPackage: "{{.AdminPackage}}/value",
			TypeScriptOutput: "Value",
			TypeScriptInput: "Value",
			TypeScriptWhere: "Value",
			GoPackage: "{{.ModulePath}}",
			GoType: "Value",
			JSONSchema: json.RawMessage(` + "`" + `{"type":"object","additionalProperties":false,"required":["value"],"properties":{"value":{"type":"string"}}}` + "`" + `),
		},
		},
	}
}

func Field(name string) field.PluginField {
	return field.Plugin(name, Key, json.RawMessage(` + "`{}`" + `))
}

func (plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: func(ctx ridu.PluginFieldValidationContext) []schema.Issue {
		if _, ok := ctx.Value.CopyObject(); ok { return nil }
		return []schema.Issue{
			{Code: "invalid_{{.GoName}}", Path: ctx.Field.Path.String(), Message: "value must be an object"},
		}
	}}
}
`,
	"plugin_test.go": `package {{.GoName}}_test

import (
	"testing"

	plugin "{{.ModulePath}}"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugintest"
	"github.com/riducms/ridu/store"
)

func TestConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: plugin.New(),
		Fields: field.Fields{plugin.Field("value")},
		ValidData: store.Values{
			"value": store.Object(store.Values{"value": store.String("valid")}),
		},
		InvalidData: store.Values{"value": store.String("invalid")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: "{{.NPMVersion}}", Compatible: true},
		},
	})
}
`,
	"admin/package.json": `{
  "name": "{{.AdminPackage}}",
  "version": "0.1.0",
  "type": "module",
  "exports": { ".": "./src/index.ts", "./value": "./src/value.ts" },
  "peerDependencies": {
    "@riducms/plugin": "{{.NPMVersion}}",
    "svelte": "^5.0.0"
  },
  "devDependencies": {
    "@riducms/plugin": "{{.NPMVersion}}",
	"@types/bun": "^1.3.0",
    "svelte": "^5.0.0",
	"svelte-check": "^4.0.0",
    "typescript": "^5.9.0"
  },
  "scripts": { "check": "svelte-check --tsconfig ./tsconfig.json", "test": "bun test" }
}
`,
	"admin/tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"module":"ESNext","moduleResolution":"Bundler","types":[]},"include":["src/**/*.ts","src/**/*.svelte"]}
`,
	"admin/src/index.ts": `import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import Field from "./field.svelte";
import { decodeValue } from "./value";
export type { Value } from "./value";

export const adminPlugin = defineAdminPlugin({
	key: "{{.Key}}",
	pairingVersion: 1,
	fields: { "{{.Key}}": definePluginField({ component: Field, decodeValue }) },
});
`,
	"admin/src/value.ts": `export interface Value { value: string }
export function decodeValue(value: unknown): Value {
	if (typeof value !== "object" || value === null || !("value" in value) || typeof value.value !== "string")
		throw new Error("Expected an object with a string value.");
	return { value: value.value };
}
`,
	"admin/src/field.svelte": `<script lang="ts">
	import type { PluginFieldProps } from "@riducms/plugin/authoring/v1";
	import type { Value } from "./value";
	let { field }: PluginFieldProps<Value> = $props();
</script>

<label for={field.schema.id}>{field.schema.admin.label}</label>
<input id={field.schema.id} value={field.value?.value ?? ""} disabled={field.readOnly} oninput={(event) => field.set({ value: event.currentTarget.value })} />
{#each field.issues as issue (issue.code)}<p>{issue.message}</p>{/each}
`,
	"admin/tests/plugin.test.ts": `import { expect, test } from "bun:test";
import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import { decodeValue } from "../src/value";

test("validates portable values before rendering", () => {
	const field = definePluginField({ component: () => ({}), decodeValue });
	const plugin = defineAdminPlugin({key: "{{.Key}}", pairingVersion: 1, fields: {"{{.Key}}": field}});
	expect(plugin.fields["{{.Key}}"].decodeValue({value: "hello"})).toEqual({value: "hello"});
	expect(() => field.decodeValue({value: 42})).toThrow();
});
`,
	"README.md": `# {{.Key}}

This scaffold is a paired, statically compiled Ridu plugin.

Run ` + "`go mod tidy && go test ./...`" + ` for backend conformance and ` + "`cd admin && bun install && bun run check && bun test`" + ` for the admin package. Keep descriptor and admin pairing versions synchronized. Store ordinary plugin records in config-contributed collections. If an exceptional database feature needs private SQL, declare it for each supported adapter, own only tables prefixed ` + "`ridu_plugin_{{.Key}}_`" + `, and test both upgrade and downgrade before publishing.
`,
}
