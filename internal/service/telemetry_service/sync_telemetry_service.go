package telemetry_service

import (
	"context"
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : SYNCHRONISATION DE LA TÉLÉMÉTRIE & TRUST SCORE (EDGE TO CLOUD)
// ############################################################################

// ProcessSyncTelemetry orchestre la synchronisation bidirectionnelle du vecteur
// comportemental, du Trust Score (ConfidenceScore serveur) et l'envoi asynchrone des Analytics aux Workers.
func ProcessSyncTelemetry(ctx context.Context, callerID int64, input telemetry_models.SyncTelemetryInput) (telemetry_models.SyncTelemetryOutput, error) {
	// Résolution en O(1) RAM du Trust Score faisant autorité côté serveur
	authoritativeTrustScore := variables.TrustScoreInitial
	if callerLite, errLite := cache_service.GetUserLite(ctx, callerID); errLite == nil && callerLite.TrustScore > 0 {
		authoritativeTrustScore = callerLite.TrustScore
	}

	syncOutput := telemetry_models.SyncTelemetryOutput{
		NeedUpdate:       false,
		Status:           "accepted",
		ServerTrustScore: authoritativeTrustScore,
		TrustTier:        computeTrustTier(authoritativeTrustScore),
	}

	// Si le score de confiance connu par le client diffère du Trust Score serveur, on signale une mise à jour requise
	if math.Abs(input.Payload.Meta.ConfidenceScore-authoritativeTrustScore) > 0.0001 {
		syncOutput.NeedUpdate = true
	}

	// ── ÉTAPE 1 : EXTRACTION DES TIMESTAMPS DE VERSIONNING ──────────────────
	clientTelemetryTimestamp := input.Payload.Meta.ClientUpdatedAt
	profile, _ := cache_service.GetTelemetryProfile(ctx, callerID)
	serverTelemetryTimestamp := profile.TimestampMs

	// ── ÉTAPE 2 : RÉSOLUTION DES CONFLITS (VECTEUR ET TAGS LSH) ─────────────
	if clientTelemetryTimestamp >= serverTelemetryTimestamp {
		// -> LE CLIENT EST L'AUTORITÉ POUR LE VECTEUR (Mais le serveur reste l'autorité pour ConfidenceScore / TrustScore)
		if len(input.Payload.Vector.Values) == variables.VectorDimTotal {
			oldVectorValues := profile.Vector
			if len(oldVectorValues) > 0 {
				// Invalidation ciblée si les clusters sémantiques changent radicalement
				algorithm_service.InvalidatePersonalizedFeedCache(ctx, callerID, oldVectorValues, input.Payload.Vector.Values)
			}

			newProfile := lite_models.TelemetryProfileLite{
				Vector:          input.Payload.Vector.Values,
				TopTags:         input.Payload.TopTags,
				ConfidenceScore: authoritativeTrustScore,
				TimestampMs:     clientTelemetryTimestamp,
			}
			_ = cache_service.SetTelemetryProfile(ctx, callerID, newProfile)
		}

		// SAUVEGARDE ASYNCHRONE DE L'ADN (EDGE-TO-CLOUD BACKUP)
		// On charge l'objet complet `UserSettings` pour ne pas écraser la confidentialité (L1)
		userSettingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, callerID)

		if errSettings == nil && userSettingsPayload.ID != 0 {
			// Injection de l'ADN algorithmique sur l'objet
			userSettingsPayload.TelemetryVector = input.Payload.Vector.Values
			userSettingsPayload.TelemetryTags = input.Payload.TopTags
			userSettingsPayload.TelemetryTimestamp = clientTelemetryTimestamp
			userSettingsPayload.UpdatedAt = domain.NowMillis()

			// 1. Écrasement synchronisé de l'Object Cache LFU (L1)
			_ = object_cache_service.SetUserSettings(ctx, userSettingsPayload)

			// 2. Délégation Write-Behind via Workers (PartitionKey = UserID pour le sharding)
			errQueue := redis.EnqueueDB(ctx, userSettingsPayload.ID, callerID, redis.EntityUserSettings, redis.ActionUpdate, userSettingsPayload, redis.TargetAll)
			if errQueue != nil {
				numan_log.Error(ctx).Err(errQueue).Int64("user_id", callerID).Msg("Échec du Write-Behind pour la sauvegarde du vecteur IA")
				// Non bloquant : la donnée est saine en RAM, le Worker tentera de survivre.
			}
		} else {
			numan_log.Warn(ctx).Err(errSettings).Int64("user_id", callerID).Msg("Impossible de charger les UserSettings pour sauvegarder le vecteur")
		}

	} else {
		// -> LE SERVEUR EST L'AUTORITÉ (Le client a du retard, il doit se mettre à jour)

		syncOutput.NeedUpdate = true
		syncOutput.Status = "server_newer"
		syncOutput.ServerUpdated = serverTelemetryTimestamp

		syncOutput.ServerVector = profile.Vector
		syncOutput.ServerTags = profile.TopTags
	}

	// ── ÉTAPE 3 : THUNDERING HERD PROTECTOR (RÉCEPTION ASYNCHRONE) ──────────

	// Traitement du batch des logs de lecture (Dwell time, Clicks, etc.)
	for _, telemetryEvent := range input.Payload.Telemetry {

		// A. Filtre Anti-Bruit (Ghost Scroll) : Si < 500ms sans interaction, on ignore
		if telemetryEvent.DwellTimeMs < variables.MaxGhostScrollTime && !telemetryEvent.IsClicked && !telemetryEvent.ProfileVisit && !telemetryEvent.DeepScroll {
			continue
		}

		// B. Détection Qualitative de la Vue (Action ou > 1.5s de lecture cognitive)
		if telemetryEvent.DwellTimeMs >= variables.MinCognitiveReadTime || telemetryEvent.IsClicked || telemetryEvent.DeepScroll || telemetryEvent.ProfileVisit {

			// MISE À JOUR SYNCHRONE DU CACHE L1 (Temps Réel)
			if targetPostPayload, errCache := object_cache_service.GetPostFromObjectCache(ctx, telemetryEvent.PostID); errCache == nil {
				targetPostPayload.ViewCount += 1
				_ = object_cache_service.SetPostInObjectCache(ctx, targetPostPayload)

				// L'algorithme réévalue l'attractivité du post pour les autres utilisateurs
				cache_service.EvaluatePostAfterView(ctx, targetPostPayload)
			}
		}

		// C. Envoi à la file d'attente (Write-Behind SQL)
		// Les Workers mettront à jour `telemetry_dwell_sum`, `view_count`, etc.
		errTelemetryQueue := redis.EnqueueDB(ctx, telemetryEvent.PostID, callerID, redis.EntityTelemetry, redis.ActionCreate, telemetryEvent, redis.TargetAll)
		if errTelemetryQueue != nil {
			numan_log.Error(ctx).Err(errTelemetryQueue).Int64("post_id", telemetryEvent.PostID).Msg("Échec du Write-Behind pour un événement de télémétrie")
			return syncOutput, numan_error.NewInternal()
		}
	}

	return syncOutput, nil
}
