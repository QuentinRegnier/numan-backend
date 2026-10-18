package email_service

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/email_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// DispatchEmail applique la matrice de confidentialité utilisateur en O(1) RAM,
// puis empile le message dans la file Redis pour traitement asynchrone par le worker.
func DispatchEmail(ctx context.Context, userID int64, targetEmail string, templateName string, subject string, data map[string]any) error {
	// 1. FILTRAGE PRIVACY L1 (Cascade L1 -> L2 -> L3)
	if userID > 0 {
		userSettings, errSettings := object_cache_service.GetUserSettingsCascade(ctx, userID)
		if errSettings != nil {
			numan_log.Warn(ctx).
				Err(errSettings).
				Int64("user_id", userID).
				Msg("DispatchEmail : impossible de vérifier les paramètres utilisateur, abandon sécurisé")
			return nil
		}

		// Si l'utilisateur a désactivé globalement les emails sortants
		if !userSettings.Notifications.MasterEmailEnabled {
			return nil
		}
	}

	// 2. PRÉPARATION DU PAYLOAD
	payload := email_models.EmailPayload{
		RecipientEmail: targetEmail,
		TemplateName:   templateName,
		Subject:        subject,
		Data:           data,
	}

	payloadBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		numan_log.Error(ctx).
			Err(errMarshal).
			Str("recipient", targetEmail).
			Str("template", templateName).
			Msg("DispatchEmail : échec de la sérialisation du payload")
		return numan_error.NewInternal(errMarshal)
	}

	// 3. ENQUEUE DANS REDIS (WRITE-BEHIND)
	if errQueue := redis.WorkerQueue.LPush(ctx, variables.EmailWorkerQueueName, payloadBytes); errQueue != nil {
		numan_log.Error(ctx).
			Err(errQueue).
			Str("recipient", targetEmail).
			Str("template", templateName).
			Msg("DispatchEmail : échec critique de l'empilement Redis")
		return numan_error.NewInternal(errQueue)
	}

	return nil
}

// DispatchSecurityEmail expédie un email critique de sécurité (OTP 2FA) sans filtrage par les préférences marketing/sociales.
func DispatchSecurityEmail(ctx context.Context, targetEmail string, templateName string, subject string, data map[string]any) error {
	payload := email_models.EmailPayload{
		RecipientEmail: targetEmail,
		TemplateName:   templateName,
		Subject:        subject,
		Data:           data,
	}
	payloadBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		numan_log.Error(ctx).Err(errMarshal).Str("recipient", targetEmail).Msg("DispatchSecurityEmail : échec de sérialisation")
		return numan_error.NewInternal(errMarshal)
	}
	if errQueue := redis.WorkerQueue.Client.LPush(ctx, variables.EmailWorkerQueueName, payloadBytes).Err(); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Str("recipient", targetEmail).Msg("DispatchSecurityEmail : échec critique LPush Redis")
		return numan_error.NewInternal(errQueue)
	}
	return nil
}
