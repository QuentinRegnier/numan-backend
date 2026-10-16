package message_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DÉTAILLÉE DES RÉACTIONS D'UN MESSAGE
// ############################################################################

// GetMessageReactions exécute le Slow Path pour récupérer la liste paginée et détaillée
// des utilisateurs ayant réagi à un message spécifique (UI Bottom Sheet).
func GetMessageReactions(ctx context.Context, callerID int64, input message_models.GetMessageReactionsInput) (message_models.GetMessageReactionsOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS AU MESSAGE (ZERO-TRUST) ──────────────────

	// Le service de sécurité s'occupe de renvoyer CodeNotFound ou CodeForbidden si nécessaire
	_, errSecurity := security_service.LeftMessage(ctx, input.MessageID, callerID)
	if errSecurity != nil {
		return message_models.GetMessageReactionsOutput{}, errSecurity
	}

	var messageReactions []message_models.MessageReactionPayload

	// ── ÉTAPE 2 : RÉCUPÉRATION DES RÉACTIONS (L2 -> L3) ─────────────────────

	// TENTATIVE L2 (MongoDB - Warm Storage)
	reactionsFromMongo, errMongo := mongo.MongoGetMessageReactionsPaginated(ctx, input.MessageID, int64(input.Limit), int64(input.Offset))
	if errMongo == nil && len(reactionsFromMongo) > 0 {
		messageReactions = reactionsFromMongo
	} else {
		// FALLBACK ABSOLU L3 (PostgreSQL - Cold Storage)
		reactionsFromPostgres, errPg := postgres.FuncGetMessageReactionsPaginated(ctx, input.MessageID, input.BeforeCreatedAt, input.BeforeID, input.Limit)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("message_id", input.MessageID).Msg("Erreur L3 lors de la récupération des réactions de message")
			return message_models.GetMessageReactionsOutput{}, numan_error.NewInternal()
		}

		messageReactions = reactionsFromPostgres

		// AUTO-GUÉRISON L3 -> L2 (Asynchrone via Queue)
		for _, reactionPayload := range messageReactions {
			go func(react message_models.MessageReactionPayload) {
				bgCtx := context.Background()
				// EntityMessageReaction va déclencher l'UPSERT côté Worker Mongo (SetUpsert: true)
				_ = redis.EnqueueDB(bgCtx, react.ID, react.MessageID, redis.EntityMessageReaction, redis.ActionUpdate, react, redis.TargetMongo)
			}(reactionPayload)
		}
	}

	// ── ÉTAPE 3 : HYDRATATION EN CASCADE (SPEED CACHE & DOMAINE MÉDIA) ──────

	var userReactionViews []message_models.UserReactionView

	for _, reactionPayload := range messageReactions {
		// Extraction en O(1) de l'empreinte utilisateur depuis le cache L1
		if userLiteData, errLite := cache_service.GetUserLite(ctx, reactionPayload.UserID); errLite == nil {

			var userAvatarView media_models.MediaView

			if userLiteData.ProfilePictureID > 0 {
				// Signature HMAC sécurisée de l'avatar via le Domaine Média
				if generatedView, errMedia := media_service.GenerateMediaViewCascade(ctx, userLiteData.ProfilePictureID, reactionPayload.UserID, 0, callerID); errMedia == nil {
					userAvatarView = generatedView
				}
			}

			userReactionViews = append(userReactionViews, message_models.UserReactionView{
				User: auth_models.UserLiteView{
					User:     userLiteData,
					Avatar:   userAvatarView,
					IsOnline: cache_service.IsUserOnline(ctx, reactionPayload.UserID),
				},
				Reaction: reactionPayload.Reaction,
			})
		}
	}

	// Prévention du retour JSON `null` sur la propriété `reactions`
	if userReactionViews == nil {
		userReactionViews = make([]message_models.UserReactionView, 0)
	}

	return message_models.GetMessageReactionsOutput{
		MessageID: input.MessageID,
		Reactions: userReactionViews,
	}, nil
}
