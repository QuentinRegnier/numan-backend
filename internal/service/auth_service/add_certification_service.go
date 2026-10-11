package auth_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// AddCertification gère le use case complet de la vérification de licence et d'attribution du badge.
func AddCertification(ctx context.Context, userID int64, input auth_models.AddCertificationInput) (auth_models.AddCertificationOutput, error) {
	// ÉTAPE 1 : SIMULATION DE L'APPEL EXTERNE AU PRESTATAIRE
	httpStatus, errCall := callFederationAPI(ctx, input.Prestataire, input.CertificationID)
	if errCall != nil || httpStatus != http.StatusOK {
		numan_log.Warn(ctx).Err(errCall).Str("prestataire", input.Prestataire).Int("http_status", httpStatus).Msg("Échec de la validation auprès de la fédération")
		return auth_models.AddCertificationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "La fédération n'a pas pu valider votre certification. Vérifiez votre ID de licence.", errCall)
	}

	// ÉTAPE 2 : RÉCUPÉRATION DE L'UTILISATEUR (Cascade L2 -> L3)
	userPayload, errMongo := mongo.MongoLoadUser(ctx, userID, "", "", "")
	if errMongo != nil || userPayload.ID == 0 {
		var errPg error
		userPayload, errPg = postgres.FuncLoadUser(ctx, userID, "", "", "")
		if errPg != nil || userPayload.ID == 0 {
			return auth_models.AddCertificationOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Profil introuvable.", errPg)
		}
	}

	// ÉTAPE 3 : VÉRIFICATION ET ATTRIBUTION DU BADGE
	// On nettoie les inputs pour éviter toute injection
	badgeName := "certification_" + pkg.CleanStr(input.Prestataire)

	if pkg.Exists(userPayload.Badges, badgeName) {
		return auth_models.AddCertificationOutput{}, numan_error.NewConflict(numan_error.CodeConflict, "Vous possédez déjà cette certification.", nil)
	}

	// Mutation
	userPayload.Badges = append(userPayload.Badges, badgeName)
	userPayload.UpdatedAt = domain.NowMillis()

	// ÉTAPE 4 : SYNCHRONISATION DES CACHES ET BDD

	// A. Éviction du cache L1 (RAM).
	// Comme vu dans ton architecture, on supprime la clé en RAM pour forcer le prochain GET
	// à recharger la donnée fraîche depuis Mongo/Postgres et l'hydrater avec ses nouveaux badges.
	_ = redis.UsersLite.DeleteObject(ctx, fmt.Sprintf("%d", userID))

	// B. Write-Behind (Propagation Asynchrone vers L2 MongoDB et L3 PostgreSQL)
	errQueue := redis.EnqueueDB(ctx, userPayload.ID, 0, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", userID).Msg("CRITICAL: Impossible d'enqueue la mise à jour des badges vers la BDD")
		return auth_models.AddCertificationOutput{}, numan_error.NewInternal()
	}

	// Succès
	numan_log.Info(ctx).Int64("user_id", userID).Str("badge", badgeName).Msg("Certification validée et attribuée avec succès.")

	return auth_models.AddCertificationOutput{
		Message: "Félicitations, votre certification a été validée !",
		Badges:  userPayload.Badges,
	}, nil
}

// callFederationAPI remplace temporairement le futur appel réseau
func callFederationAPI(ctx context.Context, prestataire string, certificationID string) (int, error) {
	numan_log.Info(ctx).Str("prestataire", prestataire).Str("cert_id", certificationID).Msg("Simulation de l'appel HTTP externe vers la fédération...")

	var response int

	switch prestataire {
	case "federation_francaise":
		// TODO: Remplacer par le véritable appel HTTP vers l'API de la fédération française
		// ex: req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.federation-francaise.com/verify", body)
		response = http.StatusOK
	case "federation_allemande":
		// TODO: Remplacer par le véritable appel HTTP vers l'API de la fédération allemande
		response = http.StatusOK
	case "federation_espagnole":
		// TODO: Remplacer par le véritable appel HTTP vers l'API de la fédération espagnole
		response = http.StatusOK
	case "federation_italienne":
		// TODO: Remplacer par le véritable appel HTTP vers l'API de la fédération italienne
		response = http.StatusOK
	default:
		return http.StatusBadRequest, errors.New("prestataire non reconnu par le système")
	}

	return response, nil
}
