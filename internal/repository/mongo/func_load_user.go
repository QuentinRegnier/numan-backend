package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

func MongoLoadUser(ctx context.Context, ID int64, Username string, Email string, Phone string) (auth_models.UserPayload, error) {
	var u auth_models.UserPayload

	if Users == nil {
		return u, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Users' is not initialized"),
		)
	}

	// Construction du filtre de recherche
	filter := make(map[string]any)
	if ID != -1 && ID != 0 {
		filter["id"] = ID // ID Snowflake
	} else if Email != "" {
		filter["email"] = Email
	} else if Username != "" {
		filter["username"] = Username
	} else if Phone != "" {
		filter["phone"] = Phone
	} else {
		return auth_models.UserPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Critères de recherche invalides.",
			errors.New("no valid search criteria provided to load user"),
		)
	}

	docs, err := Users.Get(filter, nil)
	if err != nil {
		return u, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger l'utilisateur.",
			err,
		)
	}

	if len(docs) == 0 {
		return u, numan_error.NewNotFound(
			"USER_NOT_FOUND",
			"Utilisateur introuvable (Storage L2).",
			errors.New("user was not found in Warm Storage (L2)"),
		)
	}

	// Conversion Map -> Struct
	if err := pkg.ToStruct(docs[0], &u); err != nil {
		return u, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger l'utilisateur.",
			err,
		)
	}

	return u, nil
}
