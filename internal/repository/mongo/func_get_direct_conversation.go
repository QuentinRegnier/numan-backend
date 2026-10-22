package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// MongoGetDirectConversation interroge L2 via un pipeline d'agrégation performant.
func MongoGetDirectConversation(ctx context.Context, user1, user2 int64) (conversation_models.ConversationPayload, error) {
	if Members == nil || Conversations == nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collections 'Members' or 'Conversations' are not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": bson.M{"$in": []int64{user1, user2}}}}},
		{{Key: "$group", Value: bson.M{"_id": "$conversation_id", "count": bson.M{"$sum": 1}}}},
		{{Key: "$match", Value: bson.M{"count": 2}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "messaging.conversations",
			"localField":   "_id",
			"foreignField": "id",
			"as":           "conv",
		}}},
		{{Key: "$unwind", Value: "$conv"}},
		{{Key: "$match", Value: bson.M{"conv.type": 0, "conv.state": 0}}},
		{{Key: "$limit", Value: 1}},
	}

	cursor, err := Members.DB.Collection(Members.Name).Aggregate(ctx, pipeline)
	if err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger la conversation.",
			err,
		)
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'extraire les résultats de la requête (Storage L2).",
			err,
		)
	}

	if len(results) == 0 {
		return conversation_models.ConversationPayload{}, numan_error.NewNotFound(
			"CONV_NOT_FOUND",
			"Conversation privée introuvable.",
			fmt.Errorf("direct conversation between users %d and %d was not found in Warm Storage (L2)", user1, user2),
		)
	}

	convMap, ok := results[0]["conv"].(bson.M)
	if !ok {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de formater les données de la conversation.",
			errors.New("failed to cast 'conv' field to bson.M"),
		)
	}

	var conv conversation_models.ConversationPayload
	if err := pkg.ToStruct(convMap, &conv); err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données de la conversation.",
			err,
		)
	}

	return conv, nil
}
