package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// GenerateToolFiles generates per-endpoint tool files and an aggregator file.
func GenerateToolFiles(tools []ToolData, packageName, serverURL, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// Remove old generated files before writing new ones
	if err := removeOldGeneratedFiles(outputDir); err != nil {
		return fmt.Errorf("removing old generated files: %w", err)
	}

	for _, td := range tools {
		code, err := renderToolFile(td, packageName)
		if err != nil {
			return fmt.Errorf("rendering %s: %w", td.OperationID, err)
		}
		filename := "generated_tool_" + snakeCase(td.OperationID) + ".go"
		if err := os.WriteFile(filepath.Join(outputDir, filename), []byte(code), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
	}

	agg, err := renderAggregatorFile(tools, packageName, serverURL)
	if err != nil {
		return fmt.Errorf("rendering aggregator: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "generated_tools_all.go"), []byte(agg), 0644); err != nil {
		return fmt.Errorf("writing aggregator: %w", err)
	}

	return nil
}

func removeOldGeneratedFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "generated_tool") && strings.HasSuffix(name, ".go") {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderToolFile(td ToolData, packageName string) (string, error) {
	data := struct {
		PackageName string
		Tool        ToolData
	}{packageName, td}

	return renderTemplate("tool", toolFileTmpl, data)
}

func renderAggregatorFile(tools []ToolData, packageName, serverURL string) (string, error) {
	data := struct {
		PackageName string
		ServerURL   string
		Tools       []ToolData
	}{packageName, serverURL, tools}

	return renderTemplate("aggregator", aggregatorTmpl, data)
}

func renderTemplate(name, tmplStr string, data interface{}) (string, error) {
	tmpl, err := template.New(name).Funcs(templateFuncs).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("formatting: %w\n%s", err, buf.String())
	}
	return string(formatted), nil
}

var templateFuncs = template.FuncMap{
	"quote":          func(s string) string { return fmt.Sprintf("%q", s) },
	"isPointerType":  func(goType string) bool { return strings.HasPrefix(goType, "*") },
	"pathArgExpr":    pathArgExpr,
	"queryAssign":    queryAssign,
	"bodyAssign":     bodyAssign,
	"needsStrings":   needsStrings,
	"needsJSON":      needsJSON,
}

// pathArgExpr returns the Go expression for passing a path param to the client method.
// If the type is not "string" and not a Path* alias (which are = string), we cast.
func pathArgExpr(pp ToolPathParam) string {
	if pp.GoType == "string" || strings.HasPrefix(pp.GoType, "Path") {
		return pp.GoVarName
	}
	return fmt.Sprintf("spotify.%s(%s)", pp.GoType, pp.GoVarName)
}

// needsStrings returns true if any non-JSON body tool needs "strings" import
func needsStrings(td ToolData) bool {
	return td.IsNonJSONBody
}

// needsJSON returns true if any body param has a complex type requiring JSON roundtrip
func needsJSON(td ToolData) bool {
	for _, bp := range td.BodyParams {
		if bp.IsComplexType {
			return true
		}
	}
	return false
}

// queryAssign generates the Go code to assign a query param from MCP args.
func queryAssign(p ToolParamData, toolCamelName string) string {
	if p.Required {
		return queryAssignRequired(p, toolCamelName)
	}
	return queryAssignOptional(p, toolCamelName)
}

func queryAssignRequired(p ToolParamData, toolCamelName string) string {
	switch p.MCPType {
	case "Number":
		return fmt.Sprintf("params.%s = toInt(args[%q])", p.GoFieldName, p.WireName)
	case "Boolean":
		return fmt.Sprintf("params.%s = toBool(args[%q])", p.GoFieldName, p.WireName)
	case "Array":
		elemType := sliceElemType(p.GoType)
		if elemType != "string" {
			return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tparams.%s = toTypedSlice[spotify.%s](v)\n\t\t}",
				p.WireName, p.GoFieldName, elemType)
		}
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tparams.%s = toStringSlice(v)\n\t\t}",
			p.WireName, p.GoFieldName)
	default:
		// Check if it's an enum type (not plain string/QueryMarket etc.)
		baseType := strings.TrimPrefix(p.GoType, "*")
		if isEnumType(baseType, toolCamelName) {
			return fmt.Sprintf("params.%s = spotify.%s(args[%q].(string))",
				p.GoFieldName, baseType, p.WireName)
		}
		return fmt.Sprintf("params.%s = args[%q].(string)", p.GoFieldName, p.WireName)
	}
}

func queryAssignOptional(p ToolParamData, toolCamelName string) string {
	switch p.MCPType {
	case "Number":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tn := toInt(v)\n\t\t\tparams.%s = &n\n\t\t}",
			p.WireName, p.GoFieldName)
	case "Boolean":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tb := toBool(v)\n\t\t\tparams.%s = &b\n\t\t}",
			p.WireName, p.GoFieldName)
	case "Array":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tsl := toStringSlice(v)\n\t\t\tparams.%s = sl\n\t\t}",
			p.WireName, p.GoFieldName)
	default:
		baseType := strings.TrimPrefix(p.GoType, "*")
		if isEnumType(baseType, toolCamelName) {
			return fmt.Sprintf("if v, ok := args[%q].(string); ok && v != \"\" {\n\t\t\tev := spotify.%s(v)\n\t\t\tparams.%s = &ev\n\t\t}",
				p.WireName, baseType, p.GoFieldName)
		}
		return fmt.Sprintf("if v, ok := args[%q].(string); ok && v != \"\" {\n\t\t\tparams.%s = &v\n\t\t}",
			p.WireName, p.GoFieldName)
	}
}

// bodyAssign generates the Go code to assign a body field from MCP args.
func bodyAssign(p ToolParamData) string {
	// Complex types (inline structs, maps) need JSON roundtrip for the field
	if p.IsComplexType {
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\traw, _ := json.Marshal(v)\n\t\t\t_ = json.Unmarshal(raw, &body.%s)\n\t\t}",
			p.WireName, p.GoFieldName)
	}

	if p.IsArray {
		if isPointerGoType(p.GoType) {
			return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tsl := toStringSlice(v)\n\t\t\tbody.%s = &sl\n\t\t}",
				p.WireName, p.GoFieldName)
		}
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tbody.%s = toStringSlice(v)\n\t\t}",
			p.WireName, p.GoFieldName)
	}

	if p.Required {
		return bodyAssignRequired(p)
	}
	return bodyAssignOptional(p)
}

func bodyAssignRequired(p ToolParamData) string {
	switch p.MCPType {
	case "Number":
		return fmt.Sprintf("body.%s = toInt(args[%q])", p.GoFieldName, p.WireName)
	case "Boolean":
		return fmt.Sprintf("body.%s = toBool(args[%q])", p.GoFieldName, p.WireName)
	case "Object":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tif m, ok := v.(*map[string]interface{}); ok {\n\t\t\t\tbody.%s = m\n\t\t\t}\n\t\t}",
			p.WireName, p.GoFieldName)
	default:
		return fmt.Sprintf("if v, ok := args[%q].(string); ok {\n\t\t\tbody.%s = v\n\t\t}",
			p.WireName, p.GoFieldName)
	}
}

func bodyAssignOptional(p ToolParamData) string {
	switch p.MCPType {
	case "Number":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tn := toInt(v)\n\t\t\tbody.%s = &n\n\t\t}",
			p.WireName, p.GoFieldName)
	case "Boolean":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tb := toBool(v)\n\t\t\tbody.%s = &b\n\t\t}",
			p.WireName, p.GoFieldName)
	case "Object":
		return fmt.Sprintf("if v, ok := args[%q]; ok {\n\t\t\tif m, ok := v.(*map[string]interface{}); ok {\n\t\t\t\tbody.%s = m\n\t\t\t}\n\t\t}",
			p.WireName, p.GoFieldName)
	default:
		return fmt.Sprintf("if v, ok := args[%q].(string); ok && v != \"\" {\n\t\t\tbody.%s = &v\n\t\t}",
			p.WireName, p.GoFieldName)
	}
}

func isPointerGoType(goType string) bool {
	return strings.HasPrefix(goType, "*")
}

// isEnumType checks if a Go type looks like an oapi-codegen enum type.
// Enum types follow the pattern: <ToolName>Params<FieldName> (e.g., SearchParamsType).
// Plain string, QueryMarket, etc. are NOT enum types.
func isEnumType(typeName, toolCamelName string) bool {
	if typeName == "string" || typeName == "" {
		return false
	}
	// Query* types are type aliases to string, not enums
	if strings.HasPrefix(typeName, "Query") {
		return false
	}
	// Path* types are type aliases to string, not enums
	if strings.HasPrefix(typeName, "Path") {
		return false
	}
	return true
}

// sliceElemType extracts the element type from a Go slice type.
// "[]string" -> "string", "*[]string" -> "string", "[]FooType" -> "FooType"
func sliceElemType(goType string) string {
	t := strings.TrimPrefix(goType, "*")
	t = strings.TrimPrefix(t, "[]")
	return t
}

// snakeCase converts kebab-case to snake_case for filenames.
func snakeCase(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// goReserved is the set of Go keywords and predeclared identifiers that can't
// be used as variable names.
var goReserved = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// snakeToCamel converts snake_case to camelCase (first letter lowercase)
// for use as local variable names. e.g., "playlist_id" -> "playlistId".
// Appends an underscore suffix if the result is a Go reserved keyword.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	result := strings.Join(parts, "")
	if goReserved[result] {
		result += "Param"
	}
	return result
}

// snakeToPascal converts snake_case to PascalCase (first letter uppercase)
// for use as exported struct field names. e.g., "playlist_id" -> "PlaylistId".
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func kebabToCamel(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toolDescription(summary, description string) string {
	if summary != "" && description != "" {
		return summary + "\n\n" + description
	}
	if summary != "" {
		return summary
	}
	return description
}


const toolFileTmpl = `// Code generated by cmd/codegen; DO NOT EDIT.

package {{.PackageName}}

import (
	"context"
{{- if needsJSON .Tool}}
	"encoding/json"
{{- end}}
	"fmt"
{{- if needsStrings .Tool}}
	"strings"
{{- end}}

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)

// {{.Tool.VarName}}Scopes lists the OAuth scopes required by the {{.Tool.OperationID}} tool.
var {{.Tool.VarName}}Scopes = []string{ {{- range .Tool.Scopes}}{{quote .}}, {{end -}} }

var {{.Tool.VarName}} = mcp.NewTool({{quote .Tool.OperationID}},
	mcp.WithDescription({{quote .Tool.Description}}),
{{- range .Tool.AllParams}}
	mcp.With{{.MCPType}}({{quote .WireName}}{{if .Required}}, mcp.Required(){{end}}{{if .Description}}, mcp.Description({{quote .Description}}){{end}}),
{{- end}}
)

// {{.Tool.HandlerName}} creates a handler for the {{.Tool.OperationID}} tool.
func {{.Tool.HandlerName}}(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		_ = args
{{- range .Tool.PathParams}}
		{{.GoVarName}}, _ := args[{{quote .WireName}}].(string)
{{- end}}
{{- if .Tool.ParamsTypeName}}
		params := &spotify.{{.Tool.ParamsTypeName}}{}
{{- range .Tool.QueryParams}}
		{{queryAssign . $.Tool.CamelName}}
{{- end}}
{{- end}}
{{- if .Tool.HasJSONBody}}
		var body spotify.{{.Tool.BodyTypeName}}
{{- range .Tool.BodyParams}}
		{{bodyAssign .}}
{{- end}}
{{- end}}

{{- if .Tool.IsNonJSONBody}}
		resp, err := client.{{.Tool.CamelName}}WithBodyWithResponse(ctx{{range .Tool.PathParams}}, {{pathArgExpr .}}{{end}}, {{quote .Tool.BodyContentType}}, strings.NewReader(req.GetString("body", "")))
{{- else}}
		resp, err := client.{{.Tool.CamelName}}WithResponse(ctx{{range .Tool.PathParams}}, {{pathArgExpr .}}{{end}}{{if .Tool.ParamsTypeName}}, params{{end}}{{if .Tool.HasJSONBody}}, body{{end}})
{{- end}}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if resp.HTTPResponse.StatusCode >= 400 {
			return mcp.NewToolResultError(fmt.Sprintf("Spotify API error %d: %s", resp.HTTPResponse.StatusCode, string(resp.Body))), nil
		}

		return mcp.NewToolResultText(string(resp.Body)), nil
	}
}
`

const aggregatorTmpl = `// Code generated by cmd/codegen; DO NOT EDIT.

package {{.PackageName}}

import (
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)
{{- if .ServerURL}}

// ServerURL is the Spotify API server URL extracted from the OpenAPI spec's servers block.
const ServerURL = {{quote .ServerURL}}
{{- end}}

// AllTools contains all generated MCP tool definitions.
var AllTools = []mcp.Tool{
{{- range .Tools}}
	{{.VarName}},
{{- end}}
}

// AllRegistrations returns all generated tool registrations paired with their handler factories.
func AllRegistrations() []ToolRegistration {
	return []ToolRegistration{
{{- range .Tools}}
		{Tool: {{.VarName}}, NewHandler: {{.HandlerName}}},
{{- end}}
	}
}

// AllScopes returns the deduplicated, sorted union of all OAuth scopes required by all tools.
func AllScopes() []string {
	seen := make(map[string]bool)
{{- range .Tools}}
	for _, s := range {{.VarName}}Scopes {
		seen[s] = true
	}
{{- end}}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}

// Type conversion helpers for MCP args to Go types.

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func toTypedSlice[T ~string](v interface{}) []T {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]T, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, T(s))
		}
	}
	return result
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func toBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}
`
