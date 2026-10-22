package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadUsersPaginated appelle la fonction SQL auth.func_load_users_paginated pour le Seeding L1
func FuncLoadUsersPaginated(ctx context.Context, limit, offset int) ([]lite_models.UserLiteRequest, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, username, first_name, last_name, profile_picture_id, bio, grade, conversation_permission, add_group_permission FROM auth.func_load_users_paginated($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les utilisateurs paginés.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	users := make([]lite_models.UserLiteRequest, 0)
	for rows.Next() {
		var u lite_models.UserLiteRequest
		var pp sql.NullInt64
		var bio sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &pp, &bio, &u.Grade, &u.ConversationPermission, &u.AddGroupPermission); err == nil {
			if pp.Valid {
				u.ProfilePictureID = pp.Int64
			}
			if bio.Valid {
				u.Bio = bio.String
			}
			users = append(users, u)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les utilisateurs paginés.",
			err,
		)
	}

	return users, nil
}
