package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncFilterUnreferencedArticleMedia retourne parmi candidateMediaIDs ceux qui ne sont référencés par aucun article actif (hors excludeArticleID).
func FuncFilterUnreferencedArticleMedia(ctx context.Context, candidateMediaIDs []int64, excludeArticleID int64) ([]int64, error) {
	if len(candidateMediaIDs) == 0 {
		return []int64{}, nil
	}

	if postgres.WriteDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT media_id
		FROM content.func_filter_unreferenced_article_media($1, $2)
	`

	rows, err := postgres.WriteDB().QueryContext(ctx, query, pq.Array(candidateMediaIDs), excludeArticleID)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier les références des médias d'articles.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncFilterUnreferencedArticleMedia")
		}
	}()

	unreferenced := make([]int64, 0, len(candidateMediaIDs))
	for rows.Next() {
		var mID int64
		if errScan := rows.Scan(&mID); errScan != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de décoder l'identifiant du média non référencé.",
				errScan,
			)
		}
		if mID > 0 {
			unreferenced = append(unreferenced, mID)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur lors du parcours des médias d'articles non référencés.",
			err,
		)
	}

	return unreferenced, nil
}
