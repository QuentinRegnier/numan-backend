package security_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// LeftAdmin vérifie en cascade L1 -> L2 -> L3 que l'appelant existe, est actif,
// et possède strictement le grade Administrateur (UserGradeAdmin = 4).
func LeftAdmin(ctx context.Context, callerID int64) error {
	if callerID <= 0 {
		return numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Utilisateur non authentifié.", nil)
	}

	// 1. TENTATIVE L1 (Speed Cache RAM O(1))
	if userLite, errL1 := cache_service.GetUserLite(ctx, callerID); errL1 == nil && userLite.ID != 0 {
		if userLite.Grade < variables.UserGradeAdmin {
			return numan_error.NewForbidden(
				numan_error.CodeForbidden,
				"Accès refusé : privilèges d'administrateur requis.",
				nil,
			)
		}
		return nil
	}

	// 2. FALLBACK L2 (MongoDB Warm Storage) -> L3 (PostgreSQL Cold Storage)
	userPayload, errMongo := mongo.MongoLoadUser(ctx, callerID, "", "", "")
	if errMongo != nil || userPayload.ID == 0 {
		var errPg error
		userPayload, errPg = postgres.FuncLoadUser(ctx, callerID, "", "", "")
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("user_id", callerID).Msg("Erreur L3 lors de la vérification des droits administrateur")
			return numan_error.NewInternal(errPg)
		}
		if userPayload.ID == 0 {
			return numan_error.NewNotFound(numan_error.CodeNotFound, "Compte utilisateur introuvable.", nil)
		}

		// Auto-guérison L3 -> L2 asynchrone
		go func(u auth_models.UserPayload) {
			_ = redis.EnqueueDB(context.Background(), u.ID, 0, redis.EntityUser, redis.ActionUpdate, u, redis.TargetMongo)
		}(userPayload)
	}

	if userPayload.Banned || userPayload.Desactivated {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Ce compte administrateur est suspendu ou désactivé.", nil)
	}

	if userPayload.Grade < variables.UserGradeAdmin {
		return numan_error.NewForbidden(
			numan_error.CodeForbidden,
			"Accès refusé : privilèges d'administrateur requis.",
			nil,
		)
	}

	return nil
}
