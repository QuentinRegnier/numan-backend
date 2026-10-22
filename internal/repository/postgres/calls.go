package postgres

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/lib/pq"
)

// scanPosts mutualise la logique d'itération et de scan des lignes (DRY).
func scanPosts(ctx context.Context, rows *sql.Rows) ([]post_models.PostPayload, error) {
	if rows == nil {
		return []post_models.PostPayload{}, nil
	}

	posts := make([]post_models.PostPayload, 0)
	for rows.Next() {
		var p post_models.PostPayload
		var location sql.NullString

		err := rows.Scan(
			&p.ID,
			&p.UserID,
			&p.Content,
			pq.Array(&p.Hashtags),
			pq.Array(&p.IndirectHashtags), // ✅ NOUVEAU
			pq.Array(&p.Identifiers),
			pq.Array(&p.MediaIDs),
			&p.Visibility,
			&p.PriorityLevel,
			&location,
			&p.CreatedAt,
			&p.UpdatedAt,
			&p.LikeCount,
			&p.CommentCount,
			&p.ViewCount,
			&p.ReportCount,
			&p.HasMedia,
			pq.Array(&p.Vector),
			&p.VectorVersion,
			&p.TelemetryDwellSum,
			&p.TelemetryDwellSq,
			&p.TelemetryClicks,
		)

		if err != nil {
			continue // On ignore la ligne corrompue et on passe à la suivante
		}

		if location.Valid {
			p.Location = location.String
		}

		posts = append(posts, p)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les données des publications.",
			err,
		)
	}

	return posts, nil
}
