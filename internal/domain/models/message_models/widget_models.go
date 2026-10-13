package message_models

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
)

// WidgetPayload représente les données structurées d'un message widget stockées dans attachments.widget.
type WidgetPayload struct {
	Type           string                  `json:"type" bson:"type"`
	Title          string                  `json:"title" bson:"title"`
	Description    string                  `json:"description" bson:"description"`
	Action         WidgetAction            `json:"action" bson:"action"`
	Visuals        WidgetVisuals           `json:"visuals" bson:"visuals"`
	CoverMediaView *media_models.MediaView `json:"cover_media_view,omitempty" bson:"cover_media_view,omitempty"`
}

// WidgetAction représente l'action interactive associée au widget.
type WidgetAction struct {
	Label      string `json:"label" bson:"label"`
	URL        string `json:"url" bson:"url"`
	IsExternal bool   `json:"is_external" bson:"is_external"`
}

// WidgetVisuals contient les attributs visuels et médias du widget.
type WidgetVisuals struct {
	Style        string `json:"style" bson:"style"`
	Icon         string `json:"icon" bson:"icon"`
	CoverMediaID int64  `json:"cover_media_id,omitempty" bson:"cover_media_id,omitempty"`
}

// normalizeInterfaceMap convertit récursivement les map[any]any (ex: issues de MsgPack/BSON) en map[string]any
// pour garantir une désérialisation JSON sans échec de typage de clé.
func normalizeInterfaceMap(val any) any {
	switch v := val.(type) {
	case map[any]any:
		res := make(map[string]any, len(v))
		for k, valItem := range v {
			res[fmt.Sprint(k)] = normalizeInterfaceMap(valItem)
		}
		return res
	case map[string]any:
		res := make(map[string]any, len(v))
		for k, valItem := range v {
			res[k] = normalizeInterfaceMap(valItem)
		}
		return res
	case []any:
		res := make([]any, len(v))
		for i, valItem := range v {
			res[i] = normalizeInterfaceMap(valItem)
		}
		return res
	default:
		return val
	}
}

// ParseWidgetPayload extrait et valide la structure WidgetPayload depuis une donnée brute (map, struct, etc.).
func ParseWidgetPayload(raw any) (WidgetPayload, error) {
	if raw == nil {
		return WidgetPayload{}, errors.New("le payload du widget est manquant")
	}

	switch v := raw.(type) {
	case WidgetPayload:
		return v, nil
	case *WidgetPayload:
		if v == nil {
			return WidgetPayload{}, errors.New("le payload du widget est manquant")
		}
		return *v, nil
	case []byte:
		var wp WidgetPayload
		if err := json.Unmarshal(v, &wp); err != nil {
			return WidgetPayload{}, err
		}
		return wp, nil
	case string:
		var wp WidgetPayload
		if err := json.Unmarshal([]byte(v), &wp); err != nil {
			return WidgetPayload{}, err
		}
		return wp, nil
	default:
		normalized := normalizeInterfaceMap(raw)
		data, err := json.Marshal(normalized)
		if err != nil {
			return WidgetPayload{}, err
		}
		var wp WidgetPayload
		if err := json.Unmarshal(data, &wp); err != nil {
			return WidgetPayload{}, err
		}
		return wp, nil
	}
}

// WidgetPayloadToMap convertit un WidgetPayload en map[string]any propre pour le stockage dans attachments.
func WidgetPayloadToMap(wp WidgetPayload) map[string]any {
	visualsMap := map[string]any{
		"style": wp.Visuals.Style,
		"icon":  wp.Visuals.Icon,
	}
	if wp.Visuals.CoverMediaID > 0 {
		visualsMap["cover_media_id"] = wp.Visuals.CoverMediaID
	}

	result := map[string]any{
		"type":        wp.Type,
		"title":       wp.Title,
		"description": wp.Description,
		"action": map[string]any{
			"label":       wp.Action.Label,
			"url":         wp.Action.URL,
			"is_external": wp.Action.IsExternal,
		},
		"visuals": visualsMap,
	}

	if wp.CoverMediaView != nil {
		result["cover_media_view"] = *wp.CoverMediaView
	}

	return result
}
