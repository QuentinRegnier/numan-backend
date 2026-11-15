package comment_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// ############################################################################
// # SERVICE : MISE À JOUR DE COMMENTAIRE (ÉDITION)
// ############################################################################

// UpdateComment gère la modification en récupérant l'objet complet pour le Bulk Update des workers.
func UpdateComment(ctx context.Context, callerID int64, input comment_models.UpdateCommentInput) error {

	// ── ÉTAPE 0 : VALIDATION DU PAYLOAD D'ENTRÉE ────────────────────────────
	if err := input.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 1 : VÉRIFICATION DES DROITS (SÉCURITÉ) ────────────────────────

	// LeftComment s'occupe de renvoyer CodeNotFound ou CodeForbidden proprement
	commentPayload, errSecurity := security_service.LeftComment(ctx, input.CommentID, callerID)
	if errSecurity != nil {
		return errSecurity
	}

	// ── ÉTAPE 2 : APPLICATION DES MODIFICATIONS ─────────────────────────────

	cleanContent := pkg.CleanStr(input.Content)
	if cleanContent == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le commentaire ne peut pas être vide.", nil)
	}

	commentPayload.Content = cleanContent
	commentPayload.UpdatedAt = domain.NowTime().Format(time.RFC3339)

	if err := commentPayload.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 3 : SAUVEGARDE ET DÉLÉGATION AUX WORKERS BATCH ────────────────

	// 1. Écrasement LFU immédiat en RAM
	if errCache := object_cache_service.SetCommentInObjectCache(ctx, commentPayload); errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("comment_id", commentPayload.ID).Msg("Échec de l'écrasement LFU lors de l'édition du commentaire")
	}

	// 2. Envoi de l'objet COMPLET dans la file asynchrone pour les workers (Mongo/Postgres)
	errQueue := redis.EnqueueDB(ctx, commentPayload.ID, 0, redis.EntityComment, redis.ActionUpdate, commentPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("comment_id", commentPayload.ID).Msg("Échec critique : Impossible d'enqueue la modification du commentaire")
		return numan_error.NewInternal()
	}

	return nil
}
