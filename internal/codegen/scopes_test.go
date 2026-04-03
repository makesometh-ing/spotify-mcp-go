package codegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeExtractFromSecurityDefinitions(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	scopes := ExtractScopes(spec)

	// get-playlist: playlist-read-private
	// add-tracks-to-playlist: playlist-modify-public, playlist-modify-private
	// get-playback-state: user-read-playback-state
	assert.Contains(t, scopes, "playlist-read-private")
	assert.Contains(t, scopes, "playlist-modify-public")
	assert.Contains(t, scopes, "playlist-modify-private")
	assert.Contains(t, scopes, "user-read-playback-state")
}

func TestScopeExtractUnionNoDuplicates(t *testing.T) {
	// Create operations that share scopes
	spec := &ParsedSpec{
		Operations: []Operation{
			{Scopes: []string{"scope-a", "scope-b"}},
			{Scopes: []string{"scope-b", "scope-c"}},
		},
	}
	scopes := ExtractScopes(spec)
	assert.Equal(t, []string{"scope-a", "scope-b", "scope-c"}, scopes)
}

func TestScopeExtractNoSecurityRequirements(t *testing.T) {
	spec := &ParsedSpec{
		Operations: []Operation{
			{Scopes: nil},
			{Scopes: []string{"scope-a"}},
		},
	}
	scopes := ExtractScopes(spec)
	assert.Equal(t, []string{"scope-a"}, scopes)
}

func TestScopeExtractEmptyScopeList(t *testing.T) {
	spec := &ParsedSpec{
		Operations: []Operation{
			{Scopes: []string{}},
			{Scopes: []string{"scope-a"}},
		},
	}
	scopes := ExtractScopes(spec)
	assert.Equal(t, []string{"scope-a"}, scopes)
}

func TestScopeExtractSortedAlphabetically(t *testing.T) {
	spec := &ParsedSpec{
		Operations: []Operation{
			{Scopes: []string{"zebra", "alpha", "middle"}},
		},
	}
	scopes := ExtractScopes(spec)
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, scopes)
}

func TestScopeExtractFixtureCount(t *testing.T) {
	data := loadFixture(t)
	spec, err := Parse(data)
	require.NoError(t, err)

	scopes := ExtractScopes(spec)

	// Fixture has 4 unique scopes across 3 active operations
	assert.Len(t, scopes, 4)
}
