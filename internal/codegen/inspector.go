package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// MethodInfo represents a parsed ClientWithResponses method signature.
type MethodInfo struct {
	Name           string      // e.g., "AddItemsToPlaylist" (without "WithResponse" suffix)
	PathParams     []PathParam // positional params that aren't context, params struct, body, or variadic
	ParamsTypeName string      // e.g., "AddItemsToPlaylistParams" or "" if none
	BodyTypeName   string      // e.g., "AddItemsToPlaylistJSONRequestBody" or "" if none
	IsNonJSONBody  bool        // true if this is a WithBodyWithResponse method (io.Reader body)
}

// PathParam is a positional parameter in the method signature.
type PathParam struct {
	GoName string // Go parameter name (e.g., "playlistId", "id", "pType")
	GoType string // Go type expression (e.g., "PathPlaylistId", "string")
}

// FieldInfo represents a struct field with tag metadata.
type FieldInfo struct {
	GoName   string // Go field name (e.g., "Market", "Uris")
	WireName string // wire name from form:/json: tag (e.g., "market", "uris")
	GoType   string // full Go type expression (e.g., "*string", "[]string", "*int")
	Required bool   // true if non-pointer type
	MCPType  string // "String", "Number", "Boolean", "Array"
	IsComplexType bool // true if the type contains inline structs, maps, etc.
}

// InspectResult holds everything extracted from the generated Go files.
type InspectResult struct {
	Methods     []*MethodInfo          // all ClientWithResponses methods (filtered)
	Structs     map[string][]FieldInfo // struct name -> fields
	TypeAliases map[string]string      // alias -> target (e.g., "FooJSONRequestBody" -> "FooJSONBody")
}

// ResolveBodyStruct resolves a JSONRequestBody type name to its underlying
// struct fields. It follows type aliases (e.g., FooJSONRequestBody = FooJSONBody)
// and looks up the target struct.
func (r *InspectResult) ResolveBodyStruct(typeName string) ([]FieldInfo, bool) {
	if fields, ok := r.Structs[typeName]; ok {
		return fields, true
	}
	if target, ok := r.TypeAliases[typeName]; ok {
		if fields, ok := r.Structs[target]; ok {
			return fields, true
		}
	}
	return nil, false
}

// Inspect parses oapi-codegen output files and extracts method signatures,
// struct field metadata, and type aliases.
func Inspect(clientSrc, typesSrc []byte) (*InspectResult, error) {
	fset := token.NewFileSet()

	clientFile, err := parser.ParseFile(fset, "client.go", clientSrc, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing client source: %w", err)
	}

	typesFile, err := parser.ParseFile(fset, "types.go", typesSrc, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing types source: %w", err)
	}

	result := &InspectResult{
		Structs:     make(map[string][]FieldInfo),
		TypeAliases: make(map[string]string),
	}

	// Pass 1: collect structs and type aliases from both files
	for _, file := range []*ast.File{typesFile, clientFile} {
		collectTypes(file, result)
	}

	// Pass 2: collect ClientWithResponses methods from client file
	allMethodNames := map[string]bool{}
	for _, decl := range clientFile.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || !isClientWithResponses(fd.Recv) {
			continue
		}
		if strings.HasSuffix(fd.Name.Name, "WithResponse") {
			allMethodNames[fd.Name.Name] = true
		}
	}

	for _, decl := range clientFile.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || !isClientWithResponses(fd.Recv) {
			continue
		}
		name := fd.Name.Name
		if !strings.HasSuffix(name, "WithResponse") {
			continue
		}

		isWithBody := strings.HasSuffix(name, "WithBodyWithResponse")
		if isWithBody {
			jsonVariant := strings.TrimSuffix(name, "WithBodyWithResponse") + "WithResponse"
			if allMethodNames[jsonVariant] {
				continue
			}
		}

		result.Methods = append(result.Methods, parseMethod(fd, isWithBody))
	}

	return result, nil
}

func collectTypes(file *ast.File, result *InspectResult) {
	// First pass: collect all type aliases
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Assign != 0 {
				if _, exists := result.TypeAliases[ts.Name.Name]; !exists {
					result.TypeAliases[ts.Name.Name] = typeExprString(ts.Type)
				}
			}
		}
	}
	// Second pass: collect structs and named type redefinitions
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Assign != 0 {
				continue // already handled in first pass
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				if _, exists := result.Structs[ts.Name.Name]; !exists {
					result.Structs[ts.Name.Name] = extractFields(st, result.TypeAliases)
				}
			} else if ident, ok := ts.Type.(*ast.Ident); ok {
				// Named type definition: type FooJSONRequestBody FooJSONBody
				// Treat like an alias for resolution purposes
				if _, exists := result.TypeAliases[ts.Name.Name]; !exists {
					result.TypeAliases[ts.Name.Name] = ident.Name
				}
			}
		}
	}
}

func parseMethod(fd *ast.FuncDecl, isNonJSON bool) *MethodInfo {
	name := fd.Name.Name
	if isNonJSON {
		name = strings.TrimSuffix(name, "WithBodyWithResponse")
	} else {
		name = strings.TrimSuffix(name, "WithResponse")
	}

	mi := &MethodInfo{
		Name:          name,
		IsNonJSONBody: isNonJSON,
	}

	for _, field := range fd.Type.Params.List {
		typeStr := typeExprString(field.Type)

		if typeStr == "context.Context" {
			continue
		}
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			continue
		}
		if typeStr == "io.Reader" {
			continue
		}
		if isNonJSON && typeStr == "string" && len(field.Names) > 0 && field.Names[0].Name == "contentType" {
			continue
		}

		paramName := ""
		if len(field.Names) > 0 {
			paramName = field.Names[0].Name
		}

		if star, ok := field.Type.(*ast.StarExpr); ok {
			innerType := typeExprString(star.X)
			if strings.HasSuffix(innerType, "Params") {
				mi.ParamsTypeName = innerType
				continue
			}
		}

		if strings.HasSuffix(typeStr, "JSONRequestBody") {
			mi.BodyTypeName = typeStr
			continue
		}

		mi.PathParams = append(mi.PathParams, PathParam{
			GoName: paramName,
			GoType: typeStr,
		})
	}

	return mi
}

func isClientWithResponses(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "ClientWithResponses"
}

func extractFields(st *ast.StructType, aliases map[string]string) []FieldInfo {
	var fields []FieldInfo
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue
		}
		if f.Names[0].Name == "AdditionalProperties" {
			continue
		}

		fi := FieldInfo{
			GoName:        f.Names[0].Name,
			GoType:        typeExprString(f.Type),
			Required:      !isPointerType(f.Type),
			MCPType:       goTypeToMCPType(f.Type, aliases),
			IsComplexType: isComplexType(f.Type),
		}

		if f.Tag != nil {
			tag := strings.Trim(f.Tag.Value, "`")
			fi.WireName = wireNameFromTag(tag)
		}

		fields = append(fields, fi)
	}
	return fields
}

func wireNameFromTag(tag string) string {
	st := reflect.StructTag(tag)
	for _, key := range []string{"form", "json"} {
		val, ok := st.Lookup(key)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(val, ",")
		if name != "" && name != "-" {
			return name
		}
	}
	return ""
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

// isComplexType returns true if the type expression contains inline structs,
// maps, or other types that can't be converted with simple type assertions.
func isComplexType(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return isComplexType(e.X)
	case *ast.ArrayType:
		return isComplexType(e.Elt)
	case *ast.StructType:
		return true
	case *ast.MapType:
		return true
	default:
		return false
	}
}

func goTypeToMCPType(expr ast.Expr, aliases map[string]string) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return goTypeToMCPType(e.X, aliases)
	case *ast.ArrayType:
		return "Array"
	case *ast.Ident:
		switch e.Name {
		case "int", "int32", "int64", "float32", "float64":
			return "Number"
		case "bool":
			return "Boolean"
		case "string":
			return "String"
		default:
			// Resolve type alias: QueryLimit = int, QueryOffset = int, etc.
			if target, ok := aliases[e.Name]; ok {
				switch target {
				case "int", "int32", "int64", "float32", "float64":
					return "Number"
				case "bool":
					return "Boolean"
				}
			}
			return "String"
		}
	case *ast.MapType:
		return "Object"
	default:
		return "String"
	}
}

func typeExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + typeExprString(e.X)
	case *ast.ArrayType:
		return "[]" + typeExprString(e.Elt)
	case *ast.SelectorExpr:
		return typeExprString(e.X) + "." + e.Sel.Name
	case *ast.Ellipsis:
		return "..." + typeExprString(e.Elt)
	case *ast.MapType:
		return "map[" + typeExprString(e.Key) + "]" + typeExprString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "unknown"
	}
}
