package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	libMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UserTrustScoreUpdate décrit la mise à jour atomique du TrustScore d'un utilisateur.
type UserTrustScoreUpdate struct {
	UserID                  int64
	TrustScore              float64
	QuarantinePostsApproved int
	LastInfractionAt        int64
}

// BulkUpdateUserTrustScores applique un lot de mises à jour atomiques sur auth.users via BulkWrite (§C.13).
func BulkUpdateUserTrustScores(ctx context.Context, updates []UserTrustScoreUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	if Users == nil || Users.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection Users is not initialized"),
		)
	}

	coll := Users.DB.Collection(Users.Name)
	models := make([]libMongo.WriteModel, 0, len(updates))
	for _, u := range updates {
		setFields := bson.M{
			"trust_score": u.TrustScore,
		}
		if u.QuarantinePostsApproved > 0 {
			setFields["quarantine_posts_approved"] = u.QuarantinePostsApproved
		}
		if u.LastInfractionAt > 0 {
			setFields["last_infraction_at"] = u.LastInfractionAt
		}
		model := libMongo.NewUpdateOneModel().
			SetFilter(bson.M{"id": u.UserID}).
			SetUpdate(bson.M{"$set": setFields})
		models = append(models, model)
	}

	opts := options.BulkWrite().SetOrdered(variables.MongoBulkWriteOrdered)
	_, err := coll.BulkWrite(ctx, models, opts)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'exécuter la mise à jour groupée des scores de confiance dans MongoDB.",
			err,
		)
	}
	return nil
}
