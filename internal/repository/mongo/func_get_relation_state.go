package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// MongoGetRelationState vérifie l'état de la relation dans le stockage à froid Mongo.
func MongoGetRelationState(ctx context.Context, callerID int64, targetID int64) (int, error) {
	if Relations == nil {
		return 0, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Relations' is not initialized"),
		)
	}

	// Filtre strict sur l'appelant et la cible
	filter := map[string]any{
		"caller_id": callerID,
		"target_id": targetID,
	}

	docs, err := Relations.GetPaginated(filter, nil, 0, 1)
	if err != nil {
		return 0, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'état de la relation.",
			err,
		)
	}

	if len(docs) == 0 {
		return 0, numan_error.NewNotFound(
			"RELATION_NOT_FOUND",
			"Relation introuvable dans mongo",
			fmt.Errorf("relation between caller %d and target %d was not found in Warm Storage (L2)", callerID, targetID),
		)
	}

	// Extraction robuste et défensive de l'entier "state" depuis le BSON générique
	if stateFloat, ok := docs[0]["state"].(float64); ok {
		return int(stateFloat), nil
	}
	if stateInt32, ok := docs[0]["state"].(int32); ok {
		return int(stateInt32), nil
	}
	if stateInt64, ok := docs[0]["state"].(int64); ok {
		return int(stateInt64), nil
	}
	if stateInt, ok := docs[0]["state"].(int); ok {
		return stateInt, nil
	}

	return 0, numan_error.NewAppError(
		http.StatusInternalServerError,
		numan_error.CodeDatabaseError,
		"Format de relation invalide.",
		errors.New("invalid 'state' format in MongoDB relations collection"),
	)
}
