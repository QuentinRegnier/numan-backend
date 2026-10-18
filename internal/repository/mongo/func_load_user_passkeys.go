package mongo

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadUserPasskeys charge toutes les Passkeys d'un utilisateur depuis MongoDB (Warm Storage L2).
func MongoLoadUserPasskeys(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	if Passkeys == nil {
		return nil, nil
	}

	filter := map[string]any{"user_id": userID}
	sort := map[string]any{"created_at": -1}

	docs, err := Passkeys.GetPaginated(filter, sort, 0, 20)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("user_id", userID).Msg("Erreur L2 lors du chargement des Passkeys utilisateur")
		return nil, numan_error.NewInternal()
	}

	res := make([]two_factor_models.PasskeyPayload, 0, len(docs))
	for _, doc := range docs {
		var p two_factor_models.PasskeyPayload
		if err := pkg.ToStruct(doc, &p); err == nil {
			res = append(res, p)
		}
	}
	return res, nil
}
