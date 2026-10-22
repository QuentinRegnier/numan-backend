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

// MongoLoadCommentsPaginated lit L2 en utilisant un Index Composé B-Tree ultra-rapide
func MongoLoadCommentsPaginated(ctx context.Context, postID int64, offset int64, limit int64) ([]comment_models.CommentPayload, error) {
	if Comments == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Comments' is not initialized"),
		)
	}

	// Filtre de base
	filter := bson.M{
		"post_id":    postID,
		"visibility": bson.M{"$ne": -1}, // Ignore les Soft Deletes
	}

	// Tri : -1 pour DESC, 1 pour ASC
	sort := bson.D{
		{Key: "score", Value: -1},
		{Key: "created_at", Value: 1},
	}

	docs, err := Comments.GetPaginated(filter, sort, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les commentaires.",
			err,
		)
	}

	comments := make([]comment_models.CommentPayload, 0, len(docs))
	for _, doc := range docs {
		var c comment_models.CommentPayload
		if err := pkg.ToStruct(doc, &c); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les commentaires.",
				err,
			)
		}
		comments = append(comments, c)
	}
	return comments, nil
}
