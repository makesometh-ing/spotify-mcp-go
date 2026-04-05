package codegen

import (
	"sort"
	"strings"
)

// ToolData is the unified data model for one tool, ready for template rendering.
type ToolData struct {
	OperationID     string
	CamelName       string // PascalCase, e.g., "AddItemsToPlaylist"
	VarName         string // e.g., "AddItemsToPlaylistTool"
	HandlerName     string // e.g., "NewAddItemsToPlaylistHandler"
	Summary         string
	Description     string // full description for mcp.WithDescription
	Scopes          []string
	Method          string // HTTP method
	PathPattern     string // URL path pattern
	HasBody         bool   // true if any body
	HasJSONBody     bool   // true if JSON body (use JSONRequestBody type)
	IsNonJSONBody   bool   // true if non-JSON body (use WithBodyWithResponse)
	BodyContentType string // e.g., "image/jpeg" for non-JSON
	ParamsTypeName  string // e.g., "AddItemsToPlaylistParams" or ""
	BodyTypeName    string // e.g., "AddItemsToPlaylistJSONRequestBody" or ""
	PathParams      []ToolPathParam
	QueryParams     []ToolParamData
	BodyParams      []ToolParamData
	AllParams       []ToolParamData // union for tool definition (deduplicated)
}

// ToolPathParam represents a path parameter in the handler.
type ToolPathParam struct {
	WireName    string // e.g., "playlist_id"
	GoVarName   string // e.g., "playlistId"
	GoType      string // e.g., "PathPlaylistId"
	Description string
}

// ToolParamData represents a query or body parameter.
type ToolParamData struct {
	WireName      string // e.g., "market", "uris"
	GoFieldName   string // e.g., "Market", "Uris"
	GoType        string // e.g., "*string", "[]string"
	MCPType       string // "String", "Number", "Boolean", "Array"
	Required      bool
	Description   string
	IsArray       bool // convenience: MCPType == "Array"
	IsComplexType bool // true for inline structs, maps, etc. (needs JSON roundtrip)
}

// MergeToolData joins AST inspection results with metadata to produce
// a sorted list of ToolData ready for code generation. Only operations
// present in both the AST and metadata are included.
func MergeToolData(inspect *InspectResult, meta *MetadataResult) []ToolData {
	var tools []ToolData

	for _, method := range inspect.Methods {
		opMeta, ok := meta.Operations[method.Name]
		if !ok {
			continue
		}

		td := ToolData{
			OperationID:     opMeta.OperationID,
			CamelName:       method.Name,
			VarName:         method.Name + "Tool",
			HandlerName:     "New" + method.Name + "Handler",
			Summary:         opMeta.Summary,
			Description:     toolDescription(opMeta.Summary, opMeta.Description),
			Scopes:          opMeta.Scopes,
			Method:          opMeta.Method,
			PathPattern:     opMeta.Path,
			ParamsTypeName:  method.ParamsTypeName,
			BodyTypeName:    method.BodyTypeName,
			HasBody:         method.BodyTypeName != "" || method.IsNonJSONBody,
			HasJSONBody:     method.BodyTypeName != "",
			IsNonJSONBody:   method.IsNonJSONBody,
			BodyContentType: opMeta.BodyContentType,
		}

		// Resolve path params: match AST positional params with spec param names
		specPathNames := extractPathParamNames(opMeta.Path)
		for i, pp := range method.PathParams {
			wireName := pp.GoName
			if i < len(specPathNames) {
				wireName = specPathNames[i]
			}
			td.PathParams = append(td.PathParams, ToolPathParam{
				WireName:    wireName,
				GoVarName:   pp.GoName,
				GoType:      pp.GoType,
				Description: opMeta.ParamDescs[wireName],
			})
		}

		// Collect body field wire names for collision detection
		bodyWireNames := map[string]bool{}

		// Resolve body params (if JSON body)
		if method.BodyTypeName != "" {
			bodyFields, ok := inspect.ResolveBodyStruct(method.BodyTypeName)
			if ok {
				for _, f := range bodyFields {
					bodyWireNames[f.WireName] = true
					td.BodyParams = append(td.BodyParams, ToolParamData{
						WireName:      f.WireName,
						GoFieldName:   f.GoName,
						GoType:        f.GoType,
						MCPType:       f.MCPType,
						Required:      f.Required,
						Description:   opMeta.BodyDescs[f.WireName],
						IsArray:       f.MCPType == "Array",
						IsComplexType: f.IsComplexType,
					})
				}
			}
		}

		// Resolve query params (if params struct exists)
		if method.ParamsTypeName != "" {
			if fields, ok := inspect.Structs[method.ParamsTypeName]; ok {
				for _, f := range fields {
					if bodyWireNames[f.WireName] {
						continue // body wins on collision
					}
					td.QueryParams = append(td.QueryParams, ToolParamData{
						WireName:    f.WireName,
						GoFieldName: f.GoName,
						GoType:      f.GoType,
						MCPType:     f.MCPType,
						Required:    f.Required,
						Description: opMeta.ParamDescs[f.WireName],
						IsArray:     f.MCPType == "Array",
					})
				}
			}
		}

		// Build AllParams: path + query + body, deduplicated by wire name
		seen := map[string]bool{}
		for _, pp := range td.PathParams {
			td.AllParams = append(td.AllParams, ToolParamData{
				WireName:    pp.WireName,
				MCPType:     "String",
				Required:    true,
				Description: pp.Description,
			})
			seen[pp.WireName] = true
		}
		for _, qp := range td.QueryParams {
			if !seen[qp.WireName] {
				td.AllParams = append(td.AllParams, qp)
				seen[qp.WireName] = true
			}
		}
		for _, bp := range td.BodyParams {
			if !seen[bp.WireName] {
				td.AllParams = append(td.AllParams, bp)
				seen[bp.WireName] = true
			}
		}

		tools = append(tools, td)
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].OperationID < tools[j].OperationID
	})

	return tools
}

// extractPathParamNames extracts {param_name} tokens from a URL path pattern
// in order of appearance.
func extractPathParamNames(path string) []string {
	var names []string
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			break
		}
		names = append(names, path[start+1:start+end])
		path = path[start+end+1:]
	}
	return names
}
