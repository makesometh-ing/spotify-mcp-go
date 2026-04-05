package spotify

import (
	"context"
	"io"
	"net/http"
)

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type PathPlaylistId = string
type PathArtistId = string

type ClientWithResponses struct{}

// Pattern: no params, no body
func (c *ClientWithResponses) GetCurrentUsersProfileWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path param only
func (c *ClientWithResponses) GetAnArtistWithResponse(ctx context.Context, id PathArtistId, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + query params
func (c *ClientWithResponses) GetPlaylistWithResponse(ctx context.Context, playlistId PathPlaylistId, params *GetPlaylistParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: query params only
func (c *ClientWithResponses) SearchWithResponse(ctx context.Context, params *SearchParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: body only (JSON)
func (c *ClientWithResponses) CreatePlaylistWithResponse(ctx context.Context, body CreatePlaylistJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + query + body (JSON)
func (c *ClientWithResponses) AddItemsToPlaylistWithResponse(ctx context.Context, playlistId PathPlaylistId, params *AddItemsToPlaylistParams, body AddItemsToPlaylistJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path + body (JSON), with WithBody variant
func (c *ClientWithResponses) ChangePlaylistDetailsWithBodyWithResponse(ctx context.Context, playlistId PathPlaylistId, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}
func (c *ClientWithResponses) ChangePlaylistDetailsWithResponse(ctx context.Context, playlistId PathPlaylistId, body ChangePlaylistDetailsJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: non-JSON body only (no JSON variant exists)
func (c *ClientWithResponses) UploadCustomPlaylistCoverWithBodyWithResponse(ctx context.Context, playlistId PathPlaylistId, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}

// Pattern: path param with enum type (not a Path* alias)
func (c *ClientWithResponses) GetUsersTopItemsWithResponse(ctx context.Context, pType GetUsersTopItemsParamsType, params *GetUsersTopItemsParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	return nil, nil
}
