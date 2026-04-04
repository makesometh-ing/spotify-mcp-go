package codegen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/spotify_fixture.yaml")
	require.NoError(t, err)
	return data
}

// backupFile saves the current content of a file and restores it on test cleanup.
// If the file does not exist, cleanup removes any file created at that path.
func backupFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Cleanup(func() { os.Remove(path) })
		return
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.WriteFile(path, data, 0644) })
}

func TestParserParsesValidSpec(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)
	assert.NotNil(t, spec)
	assert.NotEmpty(t, spec.Operations)
}

func TestParserExcludesDeprecated(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	for _, op := range spec.Operations {
		assert.NotEqual(t, "transfer-playback", op.OperationID,
			"deprecated operation transfer-playback should be excluded")
	}
}

func TestParserServerURL(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "https://api.spotify.com/v1", spec.ServerURL)
}

func TestParserServerURLMissing(t *testing.T) {
	spec, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0.0"
paths:
  /foo:
    get:
      operationId: get-foo
      summary: Get Foo
`))
	require.NoError(t, err)
	assert.Empty(t, spec.ServerURL)
}

func TestParserActiveOperations(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	opsByID := make(map[string]Operation)
	for _, op := range spec.Operations {
		opsByID[op.OperationID] = op
	}

	getPlaylist, ok := opsByID["get-playlist"]
	require.True(t, ok, "get-playlist should be present")
	assert.Equal(t, "GET", getPlaylist.Method)
	assert.Equal(t, "/playlists/{playlist_id}", getPlaylist.Path)

	addTracks, ok := opsByID["add-tracks-to-playlist"]
	require.True(t, ok, "add-tracks-to-playlist should be present")
	assert.Equal(t, "POST", addTracks.Method)
	assert.Equal(t, "/playlists/{playlist_id}/tracks", addTracks.Path)

	getPlayback, ok := opsByID["get-playback-state"]
	require.True(t, ok, "get-playback-state should be present")
	assert.Equal(t, "GET", getPlayback.Method)
	assert.Equal(t, "/me/player", getPlayback.Path)
}

func TestParserPathParameters(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var getPlaylist Operation
	for _, op := range spec.Operations {
		if op.OperationID == "get-playlist" {
			getPlaylist = op
			break
		}
	}

	var pathParam *Parameter
	for i, p := range getPlaylist.Parameters {
		if p.In == "path" {
			pathParam = &getPlaylist.Parameters[i]
			break
		}
	}

	require.NotNil(t, pathParam, "should have a path parameter")
	assert.Equal(t, "playlist_id", pathParam.Name)
	assert.Equal(t, "string", pathParam.Type)
}

func TestParserQueryParameters(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var getPlaylist Operation
	for _, op := range spec.Operations {
		if op.OperationID == "get-playlist" {
			getPlaylist = op
			break
		}
	}

	queryParams := make(map[string]Parameter)
	for _, p := range getPlaylist.Parameters {
		if p.In == "query" {
			queryParams[p.Name] = p
		}
	}

	market, ok := queryParams["market"]
	require.True(t, ok, "should have market query parameter")
	assert.Equal(t, "string", market.Type)
	assert.False(t, market.Required)
	assert.NotEmpty(t, market.Description)

	fields, ok := queryParams["fields"]
	require.True(t, ok, "should have fields query parameter")
	assert.Equal(t, "string", fields.Type)
	assert.False(t, fields.Required)
	assert.NotEmpty(t, fields.Description)
}

func TestParserRequestBodyRef(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var addTracks Operation
	for _, op := range spec.Operations {
		if op.OperationID == "add-tracks-to-playlist" {
			addTracks = op
			break
		}
	}

	assert.Equal(t, "#/components/schemas/AddTracksRequest", addTracks.RequestBodyRef)
}

func TestParserOperationCount(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	// Fixture has 6 operations total, 1 deprecated, 5 active
	assert.Equal(t, 5, len(spec.Operations))
}

func TestParserFetchFromURL(t *testing.T) {
	data := loadFixture(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(data)
	}))
	defer ts.Close()

	spec, err := FetchAndParse(context.Background(), ts.URL)
	require.NoError(t, err)
	assert.Equal(t, 5, len(spec.Operations))
}

func TestParserInvalidYAML(t *testing.T) {
	_, err := Parse([]byte("not: valid: yaml: {{{}"))
	assert.Error(t, err)
}

func TestParserBodyFields(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var createPlaylist Operation
	for _, op := range spec.Operations {
		if op.OperationID == "create-playlist" {
			createPlaylist = op
			break
		}
	}
	require.NotEmpty(t, createPlaylist.OperationID, "create-playlist should be present")

	// Should have body fields parsed from the CreatePlaylistRequest schema
	require.NotEmpty(t, createPlaylist.BodyFields, "should have body fields")

	fieldsByName := make(map[string]BodyField)
	for _, f := range createPlaylist.BodyFields {
		fieldsByName[f.Name] = f
	}

	// name is required string
	name, ok := fieldsByName["name"]
	require.True(t, ok, "should have 'name' body field")
	assert.Equal(t, "string", name.Type)
	assert.True(t, name.Required)
	assert.Equal(t, "The name for the new playlist.", name.Description)

	// description is optional string
	desc, ok := fieldsByName["description"]
	require.True(t, ok, "should have 'description' body field")
	assert.Equal(t, "string", desc.Type)
	assert.False(t, desc.Required)

	// public is optional boolean
	pub, ok := fieldsByName["public"]
	require.True(t, ok, "should have 'public' body field")
	assert.Equal(t, "boolean", pub.Type)
	assert.False(t, pub.Required)
}

func TestParserBodyFieldsAddTracks(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var addTracks Operation
	for _, op := range spec.Operations {
		if op.OperationID == "add-tracks-to-playlist" {
			addTracks = op
			break
		}
	}
	require.NotEmpty(t, addTracks.OperationID)

	fieldsByName := make(map[string]BodyField)
	for _, f := range addTracks.BodyFields {
		fieldsByName[f.Name] = f
	}

	// uris is an array field
	uris, ok := fieldsByName["uris"]
	require.True(t, ok, "should have 'uris' body field")
	assert.Equal(t, "array", uris.Type)
	assert.False(t, uris.Required)
	assert.Equal(t, "Spotify track URIs to add.", uris.Description)

	// position is an integer field
	pos, ok := fieldsByName["position"]
	require.True(t, ok, "should have 'position' body field")
	assert.Equal(t, "integer", pos.Type)
}

func TestParserNoBodyFieldsForGET(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var getPlaylist Operation
	for _, op := range spec.Operations {
		if op.OperationID == "get-playlist" {
			getPlaylist = op
			break
		}
	}
	assert.Empty(t, getPlaylist.BodyFields, "GET operations should have no body fields")
}

func TestParserInlineBodySchema(t *testing.T) {
	spec, err := Parse([]byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0.0"
paths:
  /things:
    post:
      operationId: create-thing
      summary: Create Thing
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required:
                - title
              properties:
                title:
                  type: string
                  description: Thing title.
                count:
                  type: integer
`))
	require.NoError(t, err)
	require.Len(t, spec.Operations, 1)

	op := spec.Operations[0]
	require.NotEmpty(t, op.BodyFields)

	fieldsByName := make(map[string]BodyField)
	for _, f := range op.BodyFields {
		fieldsByName[f.Name] = f
	}

	title, ok := fieldsByName["title"]
	require.True(t, ok)
	assert.Equal(t, "string", title.Type)
	assert.True(t, title.Required)
	assert.Equal(t, "Thing title.", title.Description)

	count, ok := fieldsByName["count"]
	require.True(t, ok)
	assert.Equal(t, "integer", count.Type)
	assert.False(t, count.Required)
}

func TestParserArrayQueryParam(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	var search Operation
	for _, op := range spec.Operations {
		if op.OperationID == "search" {
			search = op
			break
		}
	}
	require.NotEmpty(t, search.OperationID, "search should be present (not deprecated)")

	var typeParam *Parameter
	for i, p := range search.Parameters {
		if p.Name == "type" {
			typeParam = &search.Parameters[i]
			break
		}
	}
	require.NotNil(t, typeParam, "should have 'type' query parameter")
	assert.Equal(t, "array", typeParam.Type)
	assert.True(t, typeParam.HasEnum)
}
