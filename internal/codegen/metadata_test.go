package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMetadata(t *testing.T) {
	specData := loadTestFixture(t, "spotify_fixture.yaml")

	result, err := ExtractMetadata(specData)
	require.NoError(t, err)

	assert.Equal(t, "https://api.spotify.com/v1", result.ServerURL)

	// 5 active operations (transfer-playback is deprecated)
	require.Len(t, result.Operations, 5)

	t.Run("get-playlist metadata", func(t *testing.T) {
		op := result.Operations["GetPlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "get-playlist", op.OperationID)
		assert.Equal(t, "GET", op.Method)
		assert.Equal(t, "/playlists/{playlist_id}", op.Path)
		assert.Equal(t, "Get Playlist", op.Summary)
		assert.Contains(t, op.Description, "Get a playlist owned by")
		assert.Equal(t, []string{"playlist-read-private"}, op.Scopes)
		assert.Equal(t, "The Spotify ID of the playlist.", op.ParamDescs["playlist_id"])
		assert.Equal(t, "An ISO 3166-1 alpha-2 country code.", op.ParamDescs["market"])
	})

	t.Run("add-tracks-to-playlist metadata", func(t *testing.T) {
		op := result.Operations["AddTracksToPlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "add-tracks-to-playlist", op.OperationID)
		assert.Equal(t, "POST", op.Method)
		assert.Equal(t, "application/json", op.BodyContentType)
		assert.Equal(t, "Spotify track URIs to add.", op.BodyDescs["uris"])
		assert.Equal(t, "Position to insert items.", op.BodyDescs["position"])
	})

	t.Run("search metadata", func(t *testing.T) {
		op := result.Operations["Search"]
		require.NotNil(t, op)
		assert.Equal(t, "search", op.OperationID)
		assert.Equal(t, "GET", op.Method)
		assert.Empty(t, op.Scopes) // oauth_2_0: [] means no specific scopes
	})

	t.Run("create-playlist metadata", func(t *testing.T) {
		op := result.Operations["CreatePlaylist"]
		require.NotNil(t, op)
		assert.Equal(t, "create-playlist", op.OperationID)
		assert.Equal(t, "POST", op.Method)
		assert.Equal(t, "The name for the new playlist.", op.BodyDescs["name"])
	})

	t.Run("deprecated operation excluded", func(t *testing.T) {
		assert.Nil(t, result.Operations["TransferPlayback"])
	})
}
