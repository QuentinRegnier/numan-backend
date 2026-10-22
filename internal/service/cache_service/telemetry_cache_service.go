package cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : GESTION CACHE DE LA TÉLÉMÉTRIE SÉMANTIQUE
// ############################################################################

// GetTelemetryProfile récupère le profil télémétrique unifié (Vector, Tags, Confidence, Timestamp).
// Implémente le fallback L1 (Redis) -> L2 (Mongo) -> L3 (Postgres).
func GetTelemetryProfile(ctx context.Context, userID int64) (lite_models.TelemetryProfileLite, error) {
	var profile lite_models.TelemetryProfileLite

	// ── L1 : REDIS ──────────────────────────────────
	errRedis := redis.TelemetryProfiles.GetObject(ctx, userID, &profile)
	if errRedis == nil {
		return profile, nil
	}

	// ── L2 : MONGODB ────────────────────────────────
	var mongoSettings user_settings_models.UserSettingsPayload
	mongoDocs, errMongo := mongo.UserSettings.Get(map[string]any{"user_id": userID}, nil)
	if errMongo == nil && len(mongoDocs) > 0 {
		if errStruct := pkg.ToStruct(mongoDocs[0], &mongoSettings); errStruct == nil {
			profile = lite_models.TelemetryProfileLite{
				Vector:          mongoSettings.TelemetryVector,
				TopTags:         mongoSettings.TelemetryTags,
				ConfidenceScore: 0.0,
				TimestampMs:     mongoSettings.TelemetryTimestamp,
			}
			go func(p lite_models.TelemetryProfileLite, uID int64) {
				_ = SetTelemetryProfile(context.Background(), uID, p)
			}(profile, userID)
			return profile, nil
		}
	}

	// ── L3 : POSTGRESQL ─────────────────────────────
	postgresSettings, errPg := postgres.FuncLoadUserSettings(ctx, userID)
	if errPg == nil && postgresSettings.ID != 0 {
		profile = lite_models.TelemetryProfileLite{
			Vector:          postgresSettings.TelemetryVector,
			TopTags:         postgresSettings.TelemetryTags,
			ConfidenceScore: 0.0,
			TimestampMs:     postgresSettings.TelemetryTimestamp,
		}
		go func(p lite_models.TelemetryProfileLite, uID int64) {
			_ = SetTelemetryProfile(context.Background(), uID, p)
		}(profile, userID)
		return profile, nil
	}

	// ── COMPLETE MISS ───────────────────────────────
	return lite_models.TelemetryProfileLite{}, numan_error.NewNotFound(
		"TELEMETRY_SYNC_REQUIRED",
		"No trusted telemetry data available. Please synchronize the profile.",
		nil,
	)
}

// SetTelemetryProfile sauvegarde le profil de télémétrie complet (Edge-to-Cloud) avec son TTL automatique L1.
func SetTelemetryProfile(ctx context.Context, userID int64, profile lite_models.TelemetryProfileLite) error {
	errRedis := redis.TelemetryProfiles.SetObject(ctx, userID, profile)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le profil de télémétrie",
			errRedis,
		)
	}
	return nil
}
