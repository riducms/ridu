// Command goextract emits the public Go declaration surface used by the website reference
// generator. It intentionally resolves packages with go/packages and formats declarations from
// the AST while using go/types for canonical field and parameter types.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type sourceLocation struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type declarationMember struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type declaration struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Kind        string              `json:"kind"`
	Signature   string              `json:"signature"`
	Summary     string              `json:"summary"`
	Source      sourceLocation      `json:"source"`
	Members     []declarationMember `json:"members"`
	Parameters  []declarationMember `json:"parameters"`
	Returns     string              `json:"returns,omitempty"`
	Receiver    string              `json:"receiver,omitempty"`
	Aliases     []string            `json:"aliases"`
	Declaration string              `json:"declaration"`
}

type packageSurface struct {
	ImportPath   string        `json:"importPath"`
	Name         string        `json:"name"`
	Declarations []declaration `json:"declarations"`
}

type catalog struct {
	Packages []packageSurface `json:"packages"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "goextract requires at least one package pattern")
		os.Exit(2)
	}

	result, err := extract(*root, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func extract(root string, patterns []string) (catalog, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return catalog{}, fmt.Errorf("resolve repository root: %w", err)
	}
	set := token.NewFileSet()
	loaded, err := packages.Load(&packages.Config{
		Dir:  absoluteRoot,
		Fset: set,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}, patterns...)
	if err != nil {
		return catalog{}, fmt.Errorf("load Go packages: %w", err)
	}
	if packages.PrintErrors(loaded) > 0 {
		return catalog{}, fmt.Errorf("load Go packages: package errors")
	}

	result := catalog{Packages: make([]packageSurface, 0, len(loaded))}
	for _, loadedPackage := range loaded {
		surface := packageSurface{
			ImportPath:   loadedPackage.PkgPath,
			Name:         loadedPackage.Name,
			Declarations: make([]declaration, 0),
		}
		for _, file := range loadedPackage.Syntax {
			for _, node := range file.Decls {
				surface.Declarations = append(surface.Declarations, declarationsForNode(
					absoluteRoot,
					set,
					loadedPackage,
					node,
				)...)
			}
		}
		sort.SliceStable(surface.Declarations, func(left, right int) bool {
			return surface.Declarations[left].ID < surface.Declarations[right].ID
		})
		result.Packages = append(result.Packages, surface)
	}
	sort.SliceStable(result.Packages, func(left, right int) bool {
		return result.Packages[left].ImportPath < result.Packages[right].ImportPath
	})
	return result, nil
}

func declarationsForNode(root string, set *token.FileSet, loaded *packages.Package, node ast.Decl) []declaration {
	switch typed := node.(type) {
	case *ast.FuncDecl:
		if !typed.Name.IsExported() {
			return nil
		}
		if receiver := receiverName(typed.Recv); receiver != "" && !ast.IsExported(receiver) {
			return nil
		}
		return []declaration{functionDeclaration(root, set, loaded, typed)}
	case *ast.GenDecl:
		declarations := make([]declaration, 0, len(typed.Specs))
		for _, spec := range typed.Specs {
			switch exported := spec.(type) {
			case *ast.TypeSpec:
				if exported.Name.IsExported() {
					declarations = append(declarations, typeDeclaration(root, set, loaded, typed, exported))
				}
			case *ast.ValueSpec:
				for index, name := range exported.Names {
					if name.IsExported() {
						declarations = append(declarations, valueDeclaration(root, set, loaded, typed, exported, index))
					}
				}
			}
		}
		return declarations
	default:
		return nil
	}
}

func functionDeclaration(root string, set *token.FileSet, loaded *packages.Package, function *ast.FuncDecl) declaration {
	receiver := receiverName(function.Recv)
	name := function.Name.Name
	qualifiedName := name
	kind := "function"
	if receiver != "" {
		qualifiedName = receiver + "." + name
		kind = "method"
	}
	parameters := fieldListMembers(set, loaded.TypesInfo, loaded.Types, function.Type.Params, false)
	results := fieldListMembers(set, loaded.TypesInfo, loaded.Types, function.Type.Results, false)
	returnType := joinMemberTypes(results)
	return declaration{
		ID:          "go:" + loaded.PkgPath + "#" + qualifiedName,
		Name:        qualifiedName,
		Kind:        kind,
		Signature:   renderFunction(set, function, receiver),
		Summary:     documentation(function.Doc),
		Source:      locate(root, set, function.Pos()),
		Members:     []declarationMember{},
		Parameters:  parameters,
		Returns:     returnType,
		Receiver:    receiver,
		Aliases:     []string{loaded.Name + "." + qualifiedName},
		Declaration: name,
	}
}

func typeDeclaration(root string, set *token.FileSet, loaded *packages.Package, group *ast.GenDecl, spec *ast.TypeSpec) declaration {
	kind := "type"
	if _, ok := spec.Type.(*ast.InterfaceType); ok {
		kind = "interface"
	}
	members := []declarationMember{}
	switch typed := spec.Type.(type) {
	case *ast.StructType:
		members = fieldListMembers(set, loaded.TypesInfo, loaded.Types, typed.Fields, true)
	case *ast.InterfaceType:
		members = fieldListMembers(set, loaded.TypesInfo, loaded.Types, typed.Methods, true)
	}
	docs := spec.Doc
	if docs == nil {
		docs = group.Doc
	}
	return declaration{
		ID:          "go:" + loaded.PkgPath + "#" + spec.Name.Name,
		Name:        spec.Name.Name,
		Kind:        kind,
		Signature:   "type " + renderNode(set, spec),
		Summary:     documentation(docs),
		Source:      locate(root, set, spec.Pos()),
		Members:     members,
		Parameters:  []declarationMember{},
		Aliases:     []string{loaded.Name + "." + spec.Name.Name},
		Declaration: spec.Name.Name,
	}
}

func valueDeclaration(root string, set *token.FileSet, loaded *packages.Package, group *ast.GenDecl, spec *ast.ValueSpec, index int) declaration {
	name := spec.Names[index].Name
	kind := strings.ToLower(group.Tok.String())
	signature := kind + " " + name
	if spec.Type != nil {
		signature += " " + renderNode(set, spec.Type)
	}
	if index < len(spec.Values) {
		signature += " = " + renderNode(set, spec.Values[index])
	} else if len(spec.Values) == 1 {
		signature += " = " + renderNode(set, spec.Values[0])
	}
	docs := spec.Doc
	if docs == nil {
		docs = group.Doc
	}
	return declaration{
		ID:          "go:" + loaded.PkgPath + "#" + name,
		Name:        name,
		Kind:        kind,
		Signature:   signature,
		Summary:     documentation(docs),
		Source:      locate(root, set, spec.Pos()),
		Members:     []declarationMember{},
		Parameters:  []declarationMember{},
		Aliases:     []string{loaded.Name + "." + name},
		Declaration: name,
	}
}

func receiverName(receivers *ast.FieldList) string {
	if receivers == nil || len(receivers.List) == 0 {
		return ""
	}
	receiver := receivers.List[0].Type
	for {
		switch typed := receiver.(type) {
		case *ast.StarExpr:
			receiver = typed.X
		case *ast.IndexExpr:
			receiver = typed.X
		case *ast.IndexListExpr:
			receiver = typed.X
		case *ast.Ident:
			return typed.Name
		default:
			return renderNode(token.NewFileSet(), receiver)
		}
	}
}

func renderFunction(set *token.FileSet, function *ast.FuncDecl, receiver string) string {
	typeText := renderNode(set, function.Type)
	typeText = strings.TrimPrefix(typeText, "func")
	if receiver == "" {
		return "func " + function.Name.Name + typeText
	}
	receiverText := renderNode(set, function.Recv.List[0].Type)
	receiverName := "receiver"
	if len(function.Recv.List[0].Names) > 0 {
		receiverName = function.Recv.List[0].Names[0].Name
	}
	return "func (" + receiverName + " " + receiverText + ") " + function.Name.Name + typeText
}

func fieldListMembers(set *token.FileSet, information *types.Info, current *types.Package, fields *ast.FieldList, exportedOnly bool) []declarationMember {
	if fields == nil {
		return []declarationMember{}
	}
	members := make([]declarationMember, 0, len(fields.List))
	for _, field := range fields.List {
		description := documentation(field.Doc)
		if description == "" {
			description = documentation(field.Comment)
		}
		typeText := renderNode(set, field.Type)
		if objectType := information.TypeOf(field.Type); objectType != nil {
			typeText = types.TypeString(objectType, func(pkg *types.Package) string {
				if pkg == current {
					return ""
				}
				return pkg.Name()
			})
		}
		if len(field.Names) == 0 {
			if exportedOnly {
				if identifier, ok := field.Type.(*ast.Ident); ok && !identifier.IsExported() {
					continue
				}
			}
			members = append(members, declarationMember{Name: typeText, Type: typeText, Description: description})
			continue
		}
		for _, name := range field.Names {
			if exportedOnly && !name.IsExported() {
				continue
			}
			members = append(members, declarationMember{Name: name.Name, Type: typeText, Description: description})
		}
	}
	return members
}

func joinMemberTypes(members []declarationMember) string {
	if len(members) == 0 {
		return ""
	}
	parts := make([]string, 0, len(members))
	for _, member := range members {
		if member.Name != "" && member.Name != member.Type {
			parts = append(parts, member.Name+" "+member.Type)
		} else {
			parts = append(parts, member.Type)
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func renderNode(set *token.FileSet, node any) string {
	var output bytes.Buffer
	if err := format.Node(&output, set, node); err != nil {
		return ""
	}
	return strings.TrimSpace(output.String())
}

func documentation(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(group.Text())
}

func locate(root string, set *token.FileSet, position token.Pos) sourceLocation {
	resolved := set.PositionFor(position, true)
	path, err := filepath.Rel(root, resolved.Filename)
	if err != nil || strings.HasPrefix(path, "..") {
		path = resolved.Filename
	}
	return sourceLocation{Path: filepath.ToSlash(path), Line: resolved.Line}
}
