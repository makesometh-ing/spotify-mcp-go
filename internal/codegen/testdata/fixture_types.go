package spotify

type QueryMarket = string

type GetPlaylistParams struct {
	Market *QueryMarket `form:"market,omitempty" json:"market,omitempty"`
	Fields *string      `form:"fields,omitempty" json:"fields,omitempty"`
}

type SearchParams struct {
	Q    string             `form:"q" json:"q"`
	Type []SearchParamsType `form:"type" json:"type"`
}

type SearchParamsType string

type AddItemsToPlaylistParams struct {
	Position *int    `form:"position,omitempty" json:"position,omitempty"`
	Uris     *string `form:"uris,omitempty" json:"uris,omitempty"`
}

type GetUsersTopItemsParams struct {
	TimeRange *string `form:"time_range,omitempty" json:"time_range,omitempty"`
	Limit     *int    `form:"limit,omitempty" json:"limit,omitempty"`
}

type GetUsersTopItemsParamsType string

type CreatePlaylistJSONRequestBody = CreatePlaylistJSONBody
type CreatePlaylistJSONBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Public      *bool   `json:"public,omitempty"`
}

type AddItemsToPlaylistJSONRequestBody = AddItemsToPlaylistJSONBody
type AddItemsToPlaylistJSONBody struct {
	Position *int      `json:"position,omitempty"`
	Uris     *[]string `json:"uris,omitempty"`
}

type ChangePlaylistDetailsJSONRequestBody = ChangePlaylistDetailsJSONBody
type ChangePlaylistDetailsJSONBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Public      *bool   `json:"public,omitempty"`
}
