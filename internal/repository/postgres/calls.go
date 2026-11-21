package postgres

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/lib/pq"
)

// scanPosts mutualise la logique d'itération et de scan des lignes (DRY).
func scanPosts(ctx context.Context, rows *sql.Rows) ([]post_models.PostPayload, error) {
	if rows == nil {
		return []post_models.PostPayload{}, nil
	}

	cols, _ := rows.Columns()
	hasLangCol := len(cols) >= 23

	posts := make([]post_models.PostPayload, 0)
	for rows.Next() {
		var p post_models.PostPayload
		var location sql.NullString

		scanDest := []any{
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
		}
		if hasLangCol {
			scanDest = append(scanDest, &p.Lang)
		}

		err := rows.Scan(scanDest...)

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

// scanArticleRows mutualise la logique d'itération et de scan des lignes d'articles.
func scanArticleRows(rows *sql.Rows) ([]article_models.ArticlePayload, error) {
	if rows == nil {
		return []article_models.ArticlePayload{}, nil
	}

	articles := make([]article_models.ArticlePayload, 0)
	for rows.Next() {
		var a article_models.ArticlePayload
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&a.ID,
			&a.Title,
			&a.Summary,
			&a.CoverMediaID,
			&a.MarkdownMediaID,
			pq.Array(&a.AttachmentMediaIDs),
			&a.PartnerURL,
			&a.Visibility,
			&expiresAt,
			&a.CreatedAt,
			&a.UpdatedAt,
		)
		if err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de décoder les données de l'article.",
				err,
			)
		}

		if a.AttachmentMediaIDs == nil {
			a.AttachmentMediaIDs = []int64{}
		}
		if expiresAt.Valid && expiresAt.Int64 > 0 {
			val := expiresAt.Int64
			a.ExpiresAt = &val
		}

		articles = append(articles, a)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur lors du parcours des résultats des articles.",
			err,
		)
	}

	return articles, nil
}
