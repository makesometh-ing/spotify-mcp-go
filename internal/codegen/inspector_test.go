package codegen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTestFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

func TestInspect(t *testing.T) {
	clientSrc := loadTestFixture(t, "fixture_client.go")
	typesSrc := loadTestFixture(t, "fixture_types.go")

	result, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	// Should find 9 methods (ChangePlaylistDetailsWithBody is skipped because JSON variant exists)
	require.Len(t, result.Methods, 9)

	byName := make(map[string]*MethodInfo)
	for _, m := range result.Methods {
		byName[m.Name] = m
	}

	t.Run("no params no body", func(t *testing.T) {
		m := byName["GetCurrentUsersProfile"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Empty(t, m.ParamsTypeName)
		assert.Empty(t, m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
	})

	t.Run("path param only", func(t *testing.T) {
		m := byName["GetAnArtist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "id", m.PathParams[0].GoName)
		assert.Equal(t, "PathArtistId", m.PathParams[0].GoType)
		assert.Empty(t, m.ParamsTypeName)
	})

	t.Run("path plus query", func(t *testing.T) {
		m := byName["GetPlaylist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "playlistId", m.PathParams[0].GoName)
		assert.Equal(t, "GetPlaylistParams", m.ParamsTypeName)
	})

	t.Run("query only", func(t *testing.T) {
		m := byName["Search"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Equal(t, "SearchParams", m.ParamsTypeName)
	})

	t.Run("body only JSON", func(t *testing.T) {
		m := byName["CreatePlaylist"]
		require.NotNil(t, m)
		assert.Empty(t, m.PathParams)
		assert.Equal(t, "CreatePlaylistJSONRequestBody", m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
	})

	t.Run("path plus query plus body", func(t *testing.T) {
		m := byName["AddItemsToPlaylist"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "AddItemsToPlaylistParams", m.ParamsTypeName)
		assert.Equal(t, "AddItemsToPlaylistJSONRequestBody", m.BodyTypeName)
	})

	t.Run("JSON body preferred over WithBody variant", func(t *testing.T) {
		m := byName["ChangePlaylistDetails"]
		require.NotNil(t, m)
		assert.Equal(t, "ChangePlaylistDetailsJSONRequestBody", m.BodyTypeName)
		assert.False(t, m.IsNonJSONBody)
		// WithBody variant should not be a separate entry
		assert.Nil(t, byName["ChangePlaylistDetailsWithBody"])
	})

	t.Run("non-JSON body", func(t *testing.T) {
		m := byName["UploadCustomPlaylistCover"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.True(t, m.IsNonJSONBody)
		assert.Empty(t, m.BodyTypeName)
	})

	t.Run("enum path param", func(t *testing.T) {
		m := byName["GetUsersTopItems"]
		require.NotNil(t, m)
		require.Len(t, m.PathParams, 1)
		assert.Equal(t, "pType", m.PathParams[0].GoName)
		assert.Equal(t, "GetUsersTopItemsParamsType", m.PathParams[0].GoType)
		assert.Equal(t, "GetUsersTopItemsParams", m.ParamsTypeName)
	})
}

func TestInspectStructFields(t *testing.T) {
	clientSrc := loadTestFixture(t, "fixture_client.go")
	typesSrc := loadTestFixture(t, "fixture_types.go")

	result, err := Inspect(clientSrc, typesSrc)
	require.NoError(t, err)

	t.Run("query params struct", func(t *testing.T) {
		fields, ok := result.Structs["GetPlaylistParams"]
		require.True(t, ok)
		require.Len(t, fields, 2)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		market := byWire["market"]
		assert.Equal(t, "Market", market.GoName)
		assert.False(t, market.Required)
		assert.Equal(t, "String", market.MCPType)

		fields2 := byWire["fields"]
		assert.False(t, fields2.Required)
		assert.Equal(t, "String", fields2.MCPType)
	})

	t.Run("required query params", func(t *testing.T) {
		fields, ok := result.Structs["SearchParams"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		q := byWire["q"]
		assert.True(t, q.Required)
		assert.Equal(t, "String", q.MCPType)

		tp := byWire["type"]
		assert.True(t, tp.Required)
		assert.Equal(t, "Array", tp.MCPType)
	})

	t.Run("body struct via alias resolution", func(t *testing.T) {
		fields, ok := result.Structs["CreatePlaylistJSONBody"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		name := byWire["name"]
		assert.True(t, name.Required)
		assert.Equal(t, "String", name.MCPType)

		desc := byWire["description"]
		assert.False(t, desc.Required)

		pub := byWire["public"]
		assert.False(t, pub.Required)
		assert.Equal(t, "Boolean", pub.MCPType)
	})

	t.Run("body with array field", func(t *testing.T) {
		fields, ok := result.Structs["AddItemsToPlaylistJSONBody"]
		require.True(t, ok)

		byWire := make(map[string]FieldInfo)
		for _, f := range fields {
			byWire[f.WireName] = f
		}

		uris := byWire["uris"]
		assert.Equal(t, "Array", uris.MCPType)
		assert.False(t, uris.Required) // *[]string is optional
	})

	t.Run("type alias resolution", func(t *testing.T) {
		target, ok := result.TypeAliases["CreatePlaylistJSONRequestBody"]
		require.True(t, ok)
		assert.Equal(t, "CreatePlaylistJSONBody", target)
	})
}
