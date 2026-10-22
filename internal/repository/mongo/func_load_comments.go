package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoLoadComments charge une liste de commentaires depuis MongoDB via leurs IDs (Snowflake)
func MongoLoadComments(c context.Context, commentIDs []int64) ([]comment_models.CommentPayload, error) {
	if Comments == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Comments' is not initialized"),
		)
	}

	if len(commentIDs) == 0 {
		return []comment_models.CommentPayload{}, nil
	}

	// Requête stricte avec l'opérateur $in
	filter := bson.M{"id": bson.M{"$in": commentIDs}}

	// On récupère les documents bruts
	docs, err := Comments.GetPaginated(filter, nil, 0, int64(len(commentIDs)))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations des commentaires.",
			err,
		)
	}

	// Conversion propre dans le type du domaine
	comments := make([]comment_models.CommentPayload, 0, len(docs))
	for _, doc := range docs {
		var comment comment_models.CommentPayload
		if err := pkg.ToStruct(doc, &comment); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les commentaires.",
				err,
			)
		}
		comments = append(comments, comment)
	}

	return comments, nil
}
