package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadUserPasskeys charge toutes les Passkeys d'un utilisateur depuis MongoDB (Warm Storage L2).
func MongoLoadUserPasskeys(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	if Passkeys == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Passkeys' is not initialized"),
		)
	}

	filter := map[string]any{"user_id": userID}
	sort := map[string]any{"created_at": -1}

	docs, err := Passkeys.GetPaginated(filter, sort, 0, 20)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations des passkeys.",
			err,
		)
	}

	res := make([]two_factor_models.PasskeyPayload, 0, len(docs))
	for _, doc := range docs {
		var p two_factor_models.PasskeyPayload
		if err := pkg.ToStruct(doc, &p); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les passkeys.",
				err,
			)
		}
		res = append(res, p)
	}
	return res, nil
}
