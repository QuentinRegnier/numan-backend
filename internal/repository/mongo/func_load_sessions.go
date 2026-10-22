package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

func MongoLoadSession(ctx context.Context, ID int64, FirebaseInstallationID string, MasterToken string, CurrentSecret string) (auth_models.SessionsPayload, error) {
	var s auth_models.SessionsPayload

	if Sessions == nil {
		return s, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Sessions' is not initialized"),
		)
	}

	// Construction du filtre de recherche (uniquement les valeurs valides)
	filter := make(map[string]any)

	if ID != -1 && ID != 0 {
		filter["user_id"] = ID
	}
	if FirebaseInstallationID != "" {
		filter["firebase_installation_id"] = FirebaseInstallationID
	}
	if MasterToken != "" {
		filter["master_token"] = MasterToken
	}
	if CurrentSecret != "" {
		filter["current_secret"] = CurrentSecret
	}

	if len(filter) == 0 {
		return s, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Critères de recherche invalides.",
			errors.New("no valid search criteria provided to load session"),
		)
	}

	docs, err := Sessions.Get(filter, nil)
	if err != nil {
		return s, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations de session.",
			err,
		)
	}

	if len(docs) == 0 {
		return s, numan_error.NewNotFound(
			"SESSION_NOT_FOUND",
			"Session introuvable (Storage L2).",
			errors.New("session was not found in Warm Storage (L2)"),
		)
	}

	if err := pkg.ToStruct(docs[0], &s); err != nil {
		return s, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données de session.",
			err,
		)
	}

	return s, nil
}
