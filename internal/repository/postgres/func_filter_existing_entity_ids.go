package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	postgresInfra "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// Constantes d'entités autorisées par util.func_filter_existing_entity_ids.
const (
	OrphanEntityUser            = "user"
	OrphanEntityPostPhysical    = "post_physical"
	OrphanEntityPostActive      = "post_active"
	OrphanEntityCommentActive   = "comment_active"
	OrphanEntityMediaActive     = "media_active"
	OrphanEntityConversation    = "conversation"
	OrphanEntityMessageActive   = "message_active"
	OrphanEntityPOIActive       = "poi_active"
	OrphanEntityPOIReviewActive = "poi_review_active"
)

var allowedOrphanEntities = map[string]struct{}{
	OrphanEntityUser:            {},
	OrphanEntityPostPhysical:    {},
	OrphanEntityPostActive:      {},
	OrphanEntityCommentActive:   {},
	OrphanEntityMediaActive:     {},
	OrphanEntityConversation:    {},
	OrphanEntityMessageActive:   {},
	OrphanEntityPOIActive:       {},
	OrphanEntityPOIReviewActive: {},
}

// FuncFilterExistingEntityIDs vérifie en lot sur le Read Replica PostgreSQL (L3)
// quels identifiants Snowflake existent encore pour le type d'entité donné.
// Retourne une erreur typée si ReadDB n'est pas initialisée ou en cas d'échec SQL,
// afin qu'un appelant (Redis Orphan Cleaner) n'interprète JAMAIS une erreur DB comme une absence.
func FuncFilterExistingEntityIDs(ctx context.Context, entity string, ids []int64) (map[int64]bool, error) {
	if _, ok := allowedOrphanEntities[entity]; !ok {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Type d'entité non supporté pour la vérification d'existence L3.",
			errors.New("unsupported orphan check entity: "+entity),
		)
	}

	if len(ids) == 0 {
		return map[int64]bool{}, nil
	}

	db := postgresInfra.ReadDB()
	if db == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'ReadDB' is not initialized"),
		)
	}

	query := `SELECT id FROM util.func_filter_existing_entity_ids($1, $2)`
	rows, err := db.QueryContext(ctx, query, entity, pq.Array(ids))
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("entity", entity).Int("batch_size", len(ids)).Msg("Erreur SQL util.func_filter_existing_entity_ids")
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'existence des entités en base.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL util.func_filter_existing_entity_ids")
		}
	}()

	existing := make(map[int64]bool, len(ids))
	for rows.Next() {
		var id int64
		if errScan := rows.Scan(&id); errScan != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de lire les identifiants existants.",
				errScan,
			)
		}
		existing[id] = true
	}

	if errRows := rows.Err(); errRows != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les identifiants existants.",
			errRows,
		)
	}

	return existing, nil
}
