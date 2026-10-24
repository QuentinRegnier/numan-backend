package cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
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
	if errMongo != nil {
		numan_log.Warn(ctx).Err(errMongo).Int64("user_id", userID).Msg("Échec L2 lors de la récupération du profil de télémétrie")
	} else if len(mongoDocs) > 0 {
		if errStruct := pkg.ToStruct(mongoDocs[0], &mongoSettings); errStruct == nil {
			profile = lite_models.TelemetryProfileLite{
				Vector:          mongoSettings.TelemetryVector,
				TopTags:         mongoSettings.TelemetryTags,
				ConfidenceScore: 0.0,
				TimestampMs:     mongoSettings.TelemetryTimestamp,
			}
			go func(p lite_models.TelemetryProfileLite, uID int64) {
				bgCtx := context.Background()
				if err := SetTelemetryProfile(bgCtx, uID, p); err != nil {
					numan_log.Warn(bgCtx).Err(err).Int64("user_id", uID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L2->L1 du profil de télémétrie")
				}
			}(profile, userID)
			return profile, nil
		} else {
			numan_log.Warn(ctx).Err(errStruct).Int64("user_id", userID).Msg("Échec de conversion du profil de télémétrie L2")
		}
	}

	// ── L3 : POSTGRESQL ─────────────────────────────
	postgresSettings, errPg := postgres.FuncLoadUserSettings(ctx, userID)
	if errPg != nil {
		numan_log.Warn(ctx).Err(errPg).Int64("user_id", userID).Msg("Échec L3 lors de la récupération du profil de télémétrie")
	} else if postgresSettings.ID != 0 {
		profile = lite_models.TelemetryProfileLite{
			Vector:          postgresSettings.TelemetryVector,
			TopTags:         postgresSettings.TelemetryTags,
			ConfidenceScore: 0.0,
			TimestampMs:     postgresSettings.TelemetryTimestamp,
		}
		go func(p lite_models.TelemetryProfileLite, uID int64) {
			bgCtx := context.Background()
			if err := SetTelemetryProfile(bgCtx, uID, p); err != nil {
				numan_log.Warn(bgCtx).Err(err).Int64("user_id", uID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L3->L1 du profil de télémétrie")
			}
		}(profile, userID)
		return profile, nil
	}

	// ── COMPLETE MISS ───────────────────────────────
	return lite_models.TelemetryProfileLite{}, numan_error.NewNotFound(
		numan_error.CodeTelemetrySyncRequired,
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
