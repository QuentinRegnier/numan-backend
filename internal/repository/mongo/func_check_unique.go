package mongo

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
)

func MongoCheckUnique(ctx context.Context, entity redis.EntityType, field string, value any) (bool, error) {
	collection, err := redis2Mongo(ctx, entity)
	if err != nil {
		return false, err
	}

	filter := bson.M{field: value}
	projection := map[string]any{"id": 1}

	results, err := collection.Get(filter, projection)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'unicité.",
			err,
		)
	}

	return len(results) > 0, nil
}
