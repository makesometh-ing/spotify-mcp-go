package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// GenerateToolsFile generates and writes the MCP tools Go source file.
func GenerateToolsFile(ops []Operation, packageName, serverURL, outputPath string) error {
	code, err := GenerateTools(ops, packageName, serverURL)
	if err != nil {
		return err
	}
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(code), 0644)
}

// GenerateTools generates MCP tool definitions Go source code from parsed operations.
// Operations are sorted by operationID for deterministic output.
// If serverURL is non-empty, a const ServerURL is emitted in the generated code.
func GenerateTools(ops []Operation, packageName, serverURL string) (string, error) {
	sorted := make([]Operation, len(ops))
	copy(sorted, ops)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OperationID < sorted[j].OperationID
	})

	data := toolsTemplateData{
		PackageName: packageName,
		ServerURL:   serverURL,
	}
	for _, op := range sorted {
		td := newToolData(op)
		data.Tools = append(data.Tools, td)
		for _, p := range td.QueryParams {
			if p.MCPType == "Array" {
				data.HasArrayParams = true
			}
		}
		if td.HasJSONBody {
			data.HasJSONBody = true
		}
	}

	tmpl, err := template.New("tools").Funcs(template.FuncMap{
		"quote": func(s string) string { return fmt.Sprintf("%q", s) },
	}).Parse(toolsTmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("formatting generated code: %w\n%s", err, buf.String())
	}
	return string(formatted), nil
}

type toolsTemplateData struct {
	PackageName    string
	ServerURL      string
	Tools          []toolData
	HasArrayParams bool
	HasJSONBody    bool
}

type toolData struct {
	OperationID string
	CamelName   string
	VarName     string
	HandlerName string
	Description string
	Scopes      []string
	Method      string
	PathPattern string
	HasBody     bool
	HasJSONBody bool // true if body is application/json (use JSONRequestBody type)
	BodyCT      string
	Params      []toolParamData
	PathParams  []toolParamData
	QueryParams []toolParamData
	BodyParams  []toolParamData
}

type toolParamData struct {
	Name        string
	GoName      string // camelCase for local vars (e.g., "playlist_id" -> "playlistId")
	PascalName  string // PascalCase for struct fields (e.g., "playlist_id" -> "PlaylistId")
	MCPType     string
	Required    bool
	Description string
	HasEnum     bool
}

func newToolData(op Operation) toolData {
	camel := kebabToCamel(op.OperationID)
	td := toolData{
		OperationID: op.OperationID,
		CamelName:   camel,
		VarName:     camel + "Tool",
		HandlerName: "New" + camel + "Handler",
		Description: toolDescription(op.Summary, op.Description),
		Scopes:      op.Scopes,
		Method:      op.Method,
		PathPattern: op.Path,
		HasBody:     op.RequestBodyRef != "",
		HasJSONBody: op.RequestBodyRef != "" && (op.BodyContentType == "application/json" || op.BodyContentType == ""),
		BodyCT:      op.BodyContentType,
	}

	for _, p := range op.Parameters {
		pd := toolParamData{
			Name:        p.Name,
			GoName:      snakeToCamel(p.Name),
			PascalName:  snakeToPascal(p.Name),
			MCPType:     mcpType(p.Type),
			Required:    p.Required,
			Description: p.Description,
			HasEnum:     p.HasEnum,
		}
		td.Params = append(td.Params, pd)
		switch p.In {
		case "path":
			td.PathParams = append(td.PathParams, pd)
		case "query":
			td.QueryParams = append(td.QueryParams, pd)
		}
	}

	for _, f := range op.BodyFields {
		pd := toolParamData{
			Name:        f.Name,
			GoName:      snakeToCamel(f.Name),
			PascalName:  snakeToPascal(f.Name),
			MCPType:     mcpType(f.Type),
			Required:    f.Required,
			Description: f.Description,
		}
		td.BodyParams = append(td.BodyParams, pd)
		td.Params = append(td.Params, pd)
	}

	return td
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

func mcpType(openAPIType string) string {
	switch openAPIType {
	case "integer", "number":
		return "Number"
	case "boolean":
		return "Boolean"
	case "array":
		return "Array"
	default:
		return "String"
	}
}

const toolsTmpl = `// Code generated by cmd/codegen; DO NOT EDIT.

package {{.PackageName}}

import (
	"context"
{{- if .HasJSONBody}}
	"encoding/json"
{{- end}}
	"fmt"
	"sort"
{{- if .HasArrayParams}}
	"strings"
{{- end}}

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/makesometh-ing/spotify-mcp-go/internal/spotify"
)
{{- if .ServerURL}}

// ServerURL is the Spotify API server URL extracted from the OpenAPI spec's servers block.
const ServerURL = {{quote .ServerURL}}
{{- end}}

{{- range .Tools}}

// {{.VarName}}Scopes lists the OAuth scopes required by the {{.OperationID}} tool.
var {{.VarName}}Scopes = []string{ {{- range .Scopes}}{{quote .}}, {{end -}} }

var {{.VarName}} = mcp.NewTool({{quote .OperationID}},
	mcp.WithDescription({{quote .Description}}),
{{- range .Params}}
	mcp.With{{.MCPType}}({{quote .Name}}{{if .Required}}, mcp.Required(){{end}}{{if .Description}}, mcp.Description({{quote .Description}}){{end}}),
{{- end}}
)

// {{.HandlerName}} creates a handler for the {{.OperationID}} tool.
func {{.HandlerName}}(client *spotify.ClientWithResponses) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
{{- $tool := .}}
{{- range .PathParams}}
{{- if .HasEnum}}
		{{.GoName}} := spotify.{{$tool.CamelName}}Params{{.PascalName}}(req.GetString({{quote .Name}}, ""))
{{- else}}
		{{.GoName}} := req.GetString({{quote .Name}}, "")
{{- end}}
{{- end}}
{{- if .QueryParams}}
		params := &spotify.{{$tool.CamelName}}Params{}
{{- range .QueryParams}}
{{- if and .HasEnum (eq .MCPType "Array")}}
		if arr, ok := req.GetArguments()[{{quote .Name}}].([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					params.{{.PascalName}} = append(params.{{.PascalName}}, spotify.{{$tool.CamelName}}Params{{.PascalName}}(s))
				}
			}
		} else if raw := req.GetString({{quote .Name}}, ""); raw != "" {
			for _, s := range strings.Split(raw, ",") {
				if t := strings.TrimSpace(s); t != "" {
					params.{{.PascalName}} = append(params.{{.PascalName}}, spotify.{{$tool.CamelName}}Params{{.PascalName}}(t))
				}
			}
		}
{{- else if and .Required .HasEnum}}
		params.{{.PascalName}} = spotify.{{$tool.CamelName}}Params{{.PascalName}}(req.GetString({{quote .Name}}, ""))
{{- else if and .Required (eq .MCPType "Number")}}
		params.{{.PascalName}} = req.GetInt({{quote .Name}}, 0)
{{- else if and .Required (eq .MCPType "Boolean")}}
		params.{{.PascalName}} = req.GetString({{quote .Name}}, "") == "true"
{{- else if .Required}}
		params.{{.PascalName}} = req.GetString({{quote .Name}}, "")
{{- else if eq .MCPType "Number"}}
		if v := req.GetInt({{quote .Name}}, 0); v != 0 {
			params.{{.PascalName}} = &v
		}
{{- else if .HasEnum}}
		if v := req.GetString({{quote .Name}}, ""); v != "" {
			ev := spotify.{{$tool.CamelName}}Params{{.PascalName}}(v)
			params.{{.PascalName}} = &ev
		}
{{- else}}
		if v := req.GetString({{quote .Name}}, ""); v != "" {
			params.{{.PascalName}} = &v
		}
{{- end}}
{{- end}}
{{- end}}
{{- if .HasJSONBody}}
		bodyArgs := make(map[string]interface{})
		for k, v := range req.GetArguments() {
			bodyArgs[k] = v
		}
{{- range .PathParams}}
		delete(bodyArgs, {{quote .Name}})
{{- end}}
{{- range .QueryParams}}
		delete(bodyArgs, {{quote .Name}})
{{- end}}
		bodyJSON, err := json.Marshal(bodyArgs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshaling request body: %v", err)), nil
		}
		var body spotify.{{.CamelName}}JSONRequestBody
		if err := json.Unmarshal(bodyJSON, &body); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("unmarshaling request body: %v", err)), nil
		}
{{- end}}

{{- if and .HasBody (not .HasJSONBody)}}
		resp, err := client.{{.CamelName}}WithBodyWithResponse(ctx{{range .PathParams}}, {{.GoName}}{{end}}, {{quote .BodyCT}}, strings.NewReader(req.GetString("body", "")))
{{- else}}
		resp, err := client.{{.CamelName}}WithResponse(ctx{{range .PathParams}}, {{.GoName}}{{end}}{{if .QueryParams}}, params{{end}}{{if .HasJSONBody}}, body{{end}})
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
{{end}}

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
`
