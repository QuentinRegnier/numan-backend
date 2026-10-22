package mongo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// fullInboxResult est le type consolidé retournant les payloads complets depuis Mongo
type fullInboxResult struct {
	Conversation conversation_models.ConversationPayload
	Member       member_models.MemberPayload
}

// MongoLoadConversationPaginated utilise un pipeline d'agrégation pour joindre les Membres et les Conversations et trier (L2)
func MongoLoadConversationPaginated(userID int64, limit int64, offset int64) ([]fullInboxResult, error) {
	if Members == nil || Conversations == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collections 'Members' or 'Conversations' are not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{
		// 1. Filtrer les adhésions de l'utilisateur
		{{Key: "$match", Value: bson.M{"user_id": userID}}},
		// 2. Joindre la métadonnée de conversation complète
		{{Key: "$lookup", Value: bson.M{
			"from":         "messaging.conversations",
			"localField":   "conversation_id",
			"foreignField": "id",
			"as":           "conversation_meta",
		}}},
		{{Key: "$unwind", Value: "$conversation_meta"}},
		// 3. Exclure les conversations archivées/supprimées (state != 0)
		{{Key: "$match", Value: bson.M{"conversation_meta.state": 0}}},
		// 4. Trier par le dernier message ID décroissant
		{{Key: "$sort", Value: bson.M{"conversation_meta.last_message_id": -1}}},
		// 5. Paginer
		{{Key: "$skip", Value: offset}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := Members.DB.Collection(Members.Name).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les conversations.",
			err,
		)
	}
	defer cursor.Close(ctx)

	results := []fullInboxResult{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de décoder les données de conversation.",
				err,
			)
		}

		var fullConv conversation_models.ConversationPayload
		var fullMem member_models.MemberPayload

		metaDoc, ok := doc["conversation_meta"].(bson.M)
		if !ok {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Format de métadonnées de conversation invalide.",
				errors.New("failed to cast 'conversation_meta' to bson.M"),
			)
		}

		if errConv := pkg.ToStruct(metaDoc, &fullConv); errConv != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de charger les données de la conversation.",
				errConv,
			)
		}

		if errMem := pkg.ToStruct(doc, &fullMem); errMem != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de charger les données du membre.",
				errMem,
			)
		}

		results = append(results, fullInboxResult{
			Conversation: fullConv,
			Member:       fullMem,
		})
	}

	if err := cursor.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur lors du parcours des conversations.",
			err,
		)
	}

	return results, nil
}
