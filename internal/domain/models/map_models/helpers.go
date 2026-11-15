package map_models

import (
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ValidateCoordinates vérifie que la latitude et la longitude respectent le référentiel WGS84.
func ValidateCoordinates(lat, lng float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < variables.MinLatitude || lat > variables.MaxLatitude {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La latitude doit être comprise entre -90 et 90 degrés.",
			nil,
		)
	}
	if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < variables.MinLongitude || lng > variables.MaxLongitude {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La longitude doit être comprise entre -180 et 180 degrés.",
			nil,
		)
	}
	return nil
}

// ValidateBBox vérifie la cohérence géométrique d'une Bounding Box (Sud-Ouest -> Nord-Est).
func ValidateBBox(swLat, swLng, neLat, neLng float64) error {
	if err := ValidateCoordinates(swLat, swLng); err != nil {
		return err
	}
	if err := ValidateCoordinates(neLat, neLng); err != nil {
		return err
	}
	if swLat > neLat {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La latitude Sud-Ouest (sw_lat) ne peut pas être supérieure à la latitude Nord-Est (ne_lat).",
			nil,
		)
	}
	if swLng > neLng {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La longitude Sud-Ouest (sw_lng) ne peut pas être supérieure à la longitude Nord-Est (ne_lng).",
			nil,
		)
	}
	return nil
}
