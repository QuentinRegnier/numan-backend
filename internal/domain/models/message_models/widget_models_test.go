package message_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWidgetPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := func() message_models.WidgetPayload {
		return message_models.WidgetPayload{
			Type:        "action_button",
			Title:       "Rejoindre l'événement",
			Description: "Ce soir à 20h",
			Action:      message_models.WidgetAction{Label: "Voir", URL: "https://example.com"},
			Visuals:     message_models.WidgetVisuals{Style: "primary", Icon: "star", CoverMediaID: 10},
		}
	}

	var nilW *message_models.WidgetPayload
	assertBadRequest(t, nilW.Validate())
	w := valid()
	require.NoError(t, w.Validate())

	for _, tc := range []struct {
		name   string
		mutate func(*message_models.WidgetPayload)
	}{
		{"unsupported type", func(p *message_models.WidgetPayload) { p.Type = "unknown" }},
		{"title > 120 runes", func(p *message_models.WidgetPayload) { p.Title = strings.Repeat("é", 121) }},
		{"description > 500 runes", func(p *message_models.WidgetPayload) { p.Description = strings.Repeat("é", 501) }},
		{"action label > 80 runes", func(p *message_models.WidgetPayload) { p.Action.Label = strings.Repeat("a", 81) }},
		{"action url > 2048 chars", func(p *message_models.WidgetPayload) { p.Action.URL = strings.Repeat("a", 2049) }},
		{"negative cover media id", func(p *message_models.WidgetPayload) { p.Visuals.CoverMediaID = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := valid()
			tc.mutate(&cp)
			assertBadRequest(t, cp.Validate())
		})
	}
}

func TestWidgetAction_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilA *message_models.WidgetAction
	assertBadRequest(t, nilA.Validate())

	require.NoError(t, (&message_models.WidgetAction{Label: "Voir", URL: "https://example.com"}).Validate())
	require.NoError(t, (&message_models.WidgetAction{Label: strings.Repeat("é", 80), URL: strings.Repeat("a", 2048)}).Validate())
	assertBadRequest(t, (&message_models.WidgetAction{Label: strings.Repeat("é", 81)}).Validate())
	assertBadRequest(t, (&message_models.WidgetAction{URL: strings.Repeat("a", 2049)}).Validate())
}

func TestWidgetVisuals_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilV *message_models.WidgetVisuals
	assertBadRequest(t, nilV.Validate())

	require.NoError(t, (&message_models.WidgetVisuals{Style: "primary", Icon: "star", CoverMediaID: 0}).Validate())
	require.NoError(t, (&message_models.WidgetVisuals{Style: "primary", Icon: "star", CoverMediaID: 42}).Validate())
	assertBadRequest(t, (&message_models.WidgetVisuals{CoverMediaID: -1}).Validate())
}

func TestParseWidgetPayload(t *testing.T) {
	_, err := message_models.ParseWidgetPayload(nil)
	require.Error(t, err)

	var nilPtr *message_models.WidgetPayload
	_, err = message_models.ParseWidgetPayload(nilPtr)
	require.Error(t, err)

	expected := message_models.WidgetPayload{
		Type:  "poll",
		Title: "Vote",
	}

	got, err := message_models.ParseWidgetPayload(expected)
	require.NoError(t, err)
	assert.Equal(t, expected, got)

	got, err = message_models.ParseWidgetPayload(&expected)
	require.NoError(t, err)
	assert.Equal(t, expected, got)

	got, err = message_models.ParseWidgetPayload([]byte(`{"type":"link_card","title":"Doc"}`))
	require.NoError(t, err)
	assert.Equal(t, "link_card", got.Type)
	assert.Equal(t, "Doc", got.Title)

	_, err = message_models.ParseWidgetPayload([]byte(`{invalid`))
	require.Error(t, err)

	got, err = message_models.ParseWidgetPayload(`{"type":"action_button","title":"Go"}`)
	require.NoError(t, err)
	assert.Equal(t, "action_button", got.Type)
	assert.Equal(t, "Go", got.Title)

	_, err = message_models.ParseWidgetPayload(`{invalid`)
	require.Error(t, err)

	rawMap := map[any]any{
		"type":        "action_button",
		"title":       "BSON style",
		"description": "desc",
		"action": map[any]any{
			"label":       "Open",
			"url":         "https://example.com",
			"is_external": true,
		},
		"visuals": map[string]any{
			"style":          "primary",
			"icon":           "link",
			"cover_media_id": 7,
		},
		"extra": []any{map[any]any{"k": "v"}},
	}
	got, err = message_models.ParseWidgetPayload(rawMap)
	require.NoError(t, err)
	assert.Equal(t, "action_button", got.Type)
	assert.Equal(t, "BSON style", got.Title)
	assert.Equal(t, "Open", got.Action.Label)
	assert.True(t, got.Action.IsExternal)
	assert.Equal(t, int64(7), got.Visuals.CoverMediaID)

	_, err = message_models.ParseWidgetPayload(make(chan int))
	require.Error(t, err)

	_, err = message_models.ParseWidgetPayload(map[string]any{"type": 123})
	require.Error(t, err)
}

func TestWidgetPayloadToMap(t *testing.T) {
	wp := message_models.WidgetPayload{
		Type:        "action_button",
		Title:       "Title",
		Description: "Desc",
		Action: message_models.WidgetAction{
			Label:      "Click",
			URL:        "https://example.com",
			IsExternal: true,
		},
		Visuals: message_models.WidgetVisuals{
			Style: "primary",
			Icon:  "star",
		},
	}

	m := message_models.WidgetPayloadToMap(wp)
	assert.Equal(t, "action_button", m["type"])
	assert.Equal(t, "Title", m["title"])
	assert.Equal(t, "Desc", m["description"])
	assert.Equal(t, map[string]any{
		"label":       "Click",
		"url":         "https://example.com",
		"is_external": true,
	}, m["action"])
	assert.Equal(t, map[string]any{
		"style": "primary",
		"icon":  "star",
	}, m["visuals"])
	_, hasCoverView := m["cover_media_view"]
	assert.False(t, hasCoverView)

	wp.Visuals.CoverMediaID = 99
	wp.CoverMediaView = &media_models.MediaView{MediaID: 99, URL: "https://cdn.example.com/img.png"}
	m = message_models.WidgetPayloadToMap(wp)
	assert.Equal(t, map[string]any{
		"style":          "primary",
		"icon":           "star",
		"cover_media_id": int64(99),
	}, m["visuals"])
	assert.Equal(t, *wp.CoverMediaView, m["cover_media_view"])
}
