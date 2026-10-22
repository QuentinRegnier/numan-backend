package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

type timeDecayPost struct {
	ID               int64
	LikeCount        int
	CommentCount     int
	ViewCount        int
	HasMedia         bool
	CreatedAt        time.Time
	Hashtags         []string
	IndirectHashtags []string // ✅ NOUVEAU
	Visibility       int
	PriorityLevel    int
	ReportCount      int
}

func FuncLoadPostsForTimeDecay(ctx context.Context, minAge, maxAge string) ([]timeDecayPost, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, like_count, comment_count, view_count, has_media, created_at, hashtags, indirect_hashtags, visibility, priority_level, report_count FROM content.func_load_posts_for_time_decay($1::interval, $2::interval)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, minAge, maxAge)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les publications pour la décroissance temporelle.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	posts := make([]timeDecayPost, 0)
	for rows.Next() {
		var p timeDecayPost
		err := rows.Scan(
			&p.ID, &p.LikeCount, &p.CommentCount, &p.ViewCount, &p.HasMedia, &p.CreatedAt,
			pq.Array(&p.Hashtags), pq.Array(&p.IndirectHashtags), &p.Visibility, &p.PriorityLevel, &p.ReportCount, // ✅ NOUVEAU (Scan correct)
		)
		if err == nil {
			posts = append(posts, p)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les publications pour la décroissance temporelle.",
			err,
		)
	}

	return posts, nil
}
