package mongo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	libMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// BulkUpsertUserRecoSkips persiste en lot les événements de skip de recommandation d'utilisateurs
// dans la collection MongoDB "user_reco_skips" de manière idempotente (Upsert sur (user_id, skipped_user_id)).
func BulkUpsertUserRecoSkips(ctx context.Context, skips []user_recommendation_models.UserRecoSkipPayload) error {
	if len(skips) == 0 {
		return nil
	}
	if UserRecoSkips == nil || UserRecoSkips.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection UserRecoSkips is not initialized"),
		)
	}

	nowUTC := time.Now().UTC()
	models := make([]libMongo.WriteModel, 0, len(skips))

	for _, s := range skips {
		if s.UserID <= 0 || s.SkippedUserID <= 0 || s.UserID == s.SkippedUserID {
			continue
		}
		createdAt := s.CreatedAt
		if createdAt.IsZero() {
			createdAt = nowUTC
		} else {
			createdAt = createdAt.UTC()
		}

		model := libMongo.NewUpdateOneModel().
			SetFilter(bson.M{
				"user_id":         s.UserID,
				"skipped_user_id": s.SkippedUserID,
			}).
			SetUpdate(bson.M{
				"$set": bson.M{
					"user_id":         s.UserID,
					"skipped_user_id": s.SkippedUserID,
					"created_at":      createdAt,
				},
			}).
			SetUpsert(true)
		models = append(models, model)
	}

	if len(models) == 0 {
		return nil
	}

	coll := UserRecoSkips.DB.Collection(UserRecoSkips.Name)
	opts := options.BulkWrite().SetOrdered(false)
	if _, err := coll.BulkWrite(ctx, models, opts); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de persister les exclusions de recommandation dans MongoDB.",
			err,
		)
	}
	return nil
}

// MongoLoadActiveUserRecoSkips charge de manière optimisée tous les identifiants d'utilisateurs
// ignorés non expirés (< 7 jours) pour reconstruire le filtre de Cuckoo Redis après éviction.
// Ne dépend pas exclusivement de la suppression asynchrone du thread TTL MongoDB : filtre explicitement sur created_at.
func MongoLoadActiveUserRecoSkips(ctx context.Context, userID int64) ([]int64, error) {
	if userID <= 0 {
		return []int64{}, nil
	}
	if UserRecoSkips == nil || UserRecoSkips.DB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection UserRecoSkips is not initialized"),
		)
	}

	minCreatedAt := time.Now().UTC().Add(-time.Duration(variables.UserRecoSkipTTLSeconds) * time.Second)
	filter := bson.M{
		"user_id":    userID,
		"created_at": bson.M{"$gte": minCreatedAt},
	}
	findOpts := options.Find().SetProjection(bson.M{
		"skipped_user_id": 1,
		"_id":             0,
	})

	coll := UserRecoSkips.DB.Collection(UserRecoSkips.Name)
	cur, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger l'historique des exclusions de recommandation.",
			err,
		)
	}
	defer cur.Close(ctx)

	type skipProjection struct {
		SkippedUserID int64 `bson:"skipped_user_id"`
	}
	var rows []skipProjection
	if err := cur.All(ctx, &rows); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de décoder l'historique des exclusions de recommandation.",
			err,
		)
	}

	seen := make(map[int64]struct{}, len(rows))
	result := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.SkippedUserID <= 0 || r.SkippedUserID == userID {
			continue
		}
		if _, exists := seen[r.SkippedUserID]; !exists {
			seen[r.SkippedUserID] = struct{}{}
			result = append(result, r.SkippedUserID)
		}
	}

	return result, nil
}
