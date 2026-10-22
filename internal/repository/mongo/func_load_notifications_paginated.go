package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoLoadNotificationsPaginated récupère l'historique d'un utilisateur par lots (L2 Fallback absolu).
// Le tri se fait par ordre chronologique décroissant (les plus récentes en premier).
func MongoLoadNotificationsPaginated(c context.Context, userID int64, limit int64, offset int64) ([]notification_models.NotificationPayload, error) {
	if Notifications == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Notifications' is not initialized"),
		)
	}

	filter := map[string]any{"user_id": userID}
	sort := map[string]any{"created_at": -1}

	docs, err := Notifications.GetPaginated(filter, sort, offset, limit)
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
