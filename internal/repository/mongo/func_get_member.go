package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoGetMember récupère le payload complet d'un membre avec le Smart Fallback (Retard BDD)
func MongoGetMember(ctx context.Context, convID int64, userID int64) (member_models.MemberPayload, error) {
	var mem member_models.MemberPayload

	if Members == nil || Messages == nil {
		return mem, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collections 'Members' or 'Messages' are not initialized"),
		)
	}

	filter := map[string]any{"conversation_id": convID, "user_id": userID}
	docs, err := Members.Get(filter, nil)
	if err != nil {
		return mem, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données du membre.",
			err,
		)
	}

	if len(docs) == 0 {
		return mem, numan_error.NewNotFound(
			"MEMBER_NOT_FOUND",
			"Membre introuvable (Storage L2).",
			fmt.Errorf("member with user ID %d in conversation %d was not found in Warm Storage (L2)", userID, convID),
		)
	}

	if err := pkg.ToStruct(docs[0], &mem); err != nil {
		return mem, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données du membre.",
			err,
		)
	}

	// SMART RE-COUNT : Si le compteur est à 0 ou qu'on a plus de 5s de retard
	if mem.UnreadCount == 0 || time.Since(domain.MillisToTime(mem.UpdatedAt)) > 5*time.Second {
		countFilter := map[string]any{
			"conversation_id": convID,
			"sender_id":       map[string]any{"$ne": userID},
			"created_at":      map[string]any{"$gt": mem.UpdatedAt}, // Strictement après la dernière sauvegarde
		}

		if Messages.DB != nil {
			realUnread, errCount := Messages.DB.Collection(Messages.Name).CountDocuments(ctx, countFilter)
			if errCount == nil {
				mem.UnreadCount += int(realUnread)
			}
		}
	}

	return mem, nil
}
