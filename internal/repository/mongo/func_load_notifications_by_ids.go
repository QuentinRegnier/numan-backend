package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadNotificationsByIDs récupère un lot précis de notifications pour l'hydratation L1.
func MongoLoadNotificationsByIDs(c context.Context, ids []int64) ([]notification_models.NotificationPayload, error) {
	if Notifications == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Notifications' is not initialized"),
		)
	}

	if len(ids) == 0 {
		return []notification_models.NotificationPayload{}, nil
	}

	filter := map[string]any{"id": map[string]any{"$in": ids}}

	docs, err := Notifications.Get(filter, nil)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations des notifications.",
			err,
		)
	}

	notifs := make([]notification_models.NotificationPayload, 0, len(docs))
	for _, doc := range docs {
		var n notification_models.NotificationPayload
		if err := pkg.ToStruct(doc, &n); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les notifications.",
				err,
			)
		}
		notifs = append(notifs, n)
	}
	return notifs, nil
}
