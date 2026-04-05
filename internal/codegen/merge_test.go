package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeToolData(t *testing.T) {
	clientSrc := loadTestFixture(t, "fixture_client.go")
	typesSrc := loadTestFixture(t, "fixture_types.go")
	specData := loadTestFixture(t, "spotify_fixture.yaml")

	inspect, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	meta, err := ExtractMetadata(specData)
	require.NoError(t, err)

	tools := MergeToolData(inspect, meta)

	// Only operations present in BOTH AST and metadata appear.
	// Fixture client has: GetCurrentUsersProfile, GetAnArtist, GetPlaylist,
	// Search, CreatePlaylist, AddItemsToPlaylist, ChangePlaylistDetails,
	// UploadCustomPlaylistCover, GetUsersTopItems.
	// Fixture spec has: GetPlaylist, AddTracksToPlaylist, GetPlaybackState,
	// CreatePlaylist, Search.
	// Intersection: GetPlaylist, CreatePlaylist, Search.
	require.Len(t, tools, 3)

	byID := make(map[string]*ToolData)
	for i := range tools {
		byID[tools[i].OperationID] = &tools[i]
	}

	t.Run("get-playlist merged", func(t *testing.T) {
		td := byID["get-playlist"]
		require.NotNil(t, td)
		assert.Equal(t, "GetPlaylist", td.CamelName)
		assert.Equal(t, "Get Playlist", td.Summary)
		assert.Contains(t, td.Description, "Get a playlist owned by")
		assert.Equal(t, "GET", td.Method)
		assert.Equal(t, "/playlists/{playlist_id}", td.PathPattern)

		require.Len(t, td.PathParams, 1)
		assert.Equal(t, "playlist_id", td.PathParams[0].WireName)
		assert.Equal(t, "playlistId", td.PathParams[0].GoVarName)
		assert.Equal(t, "PathPlaylistId", td.PathParams[0].GoType)

		require.Len(t, td.QueryParams, 2)
	})

	t.Run("search merged", func(t *testing.T) {
		td := byID["search"]
		require.NotNil(t, td)
		assert.Empty(t, td.PathParams)
		require.Len(t, td.QueryParams, 2)

		byWire := make(map[string]*ToolParamData)
		for i := range td.QueryParams {
			byWire[td.QueryParams[i].WireName] = &td.QueryParams[i]
		}

		q := byWire["q"]
		require.NotNil(t, q)
		assert.True(t, q.Required)
		assert.Equal(t, "String", q.MCPType)
		assert.Equal(t, "Search query keywords.", q.Description)

		tp := byWire["type"]
		require.NotNil(t, tp)
		assert.True(t, tp.Required)
		assert.Equal(t, "Array", tp.MCPType)
	})

	t.Run("create-playlist merged", func(t *testing.T) {
		td := byID["create-playlist"]
		require.NotNil(t, td)
		assert.True(t, td.HasJSONBody)
		require.Len(t, td.BodyParams, 3)

		byWire := make(map[string]*ToolParamData)
		for i := range td.BodyParams {
			byWire[td.BodyParams[i].WireName] = &td.BodyParams[i]
		}

		name := byWire["name"]
		require.NotNil(t, name)
		assert.True(t, name.Required)
		assert.Equal(t, "The name for the new playlist.", name.Description)
	})

	t.Run("sorted by operation ID", func(t *testing.T) {
		assert.True(t, tools[0].OperationID < tools[1].OperationID)
		assert.True(t, tools[1].OperationID < tools[2].OperationID)
	})
}

func TestMergeNameCollision(t *testing.T) {
	// AddItemsToPlaylist has "position" and "uris" in both query params and body.
	// Body should win; query params with colliding names should be excluded.
	// We can't test this with the fixture YAML because AddItemsToPlaylist
	// in the fixture spec uses a different operationId (add-tracks-to-playlist).
	// So we test the collision logic directly with constructed data.

	inspect := &InspectResult{
		Methods: []*MethodInfo{
			{
				Name:           "AddItemsToPlaylist",
				PathParams:     []PathParam{{GoName: "playlistId", GoType: "PathPlaylistId"}},
				ParamsTypeName: "AddItemsToPlaylistParams",
				BodyTypeName:   "AddItemsToPlaylistJSONRequestBody",
			},
		},
		Structs: map[string][]FieldInfo{
			"AddItemsToPlaylistParams": {
				{GoName: "Position", WireName: "position", GoType: "*int", Required: false, MCPType: "Number"},
				{GoName: "Uris", WireName: "uris", GoType: "*string", Required: false, MCPType: "String"},
			},
			"AddItemsToPlaylistJSONBody": {
				{GoName: "Position", WireName: "position", GoType: "*int", Required: false, MCPType: "Number"},
				{GoName: "Uris", WireName: "uris", GoType: "*[]string", Required: false, MCPType: "Array"},
			},
		},
		TypeAliases: map[string]string{
			"AddItemsToPlaylistJSONRequestBody": "AddItemsToPlaylistJSONBody",
		},
	}

	meta := &MetadataResult{
		Operations: map[string]*OperationMeta{
			"AddItemsToPlaylist": {
				OperationID:     "add-items-to-playlist",
				Method:          "POST",
				Path:            "/playlists/{playlist_id}/tracks",
				Summary:         "Add Items",
				ParamDescs:      map[string]string{"playlist_id": "The playlist ID"},
				BodyDescs:       map[string]string{"uris": "URIs to add", "position": "Insert position"},
				BodyContentType: "application/json",
			},
		},
	}

	tools := MergeToolData(inspect, meta)
	require.Len(t, tools, 1)

	td := tools[0]

	// Body wins: uris should be Array (from body), not String (from query)
	assert.Len(t, td.BodyParams, 2)
	assert.Empty(t, td.QueryParams, "query params with colliding names should be excluded")

	// AllParams should have playlist_id + position + uris = 3, no duplicates
	assert.Len(t, td.AllParams, 3)

	wireNames := make(map[string]int)
	for _, p := range td.AllParams {
		wireNames[p.WireName]++
	}
	for name, count := range wireNames {
		assert.Equal(t, 1, count, "param %q should appear exactly once", name)
	}
}
