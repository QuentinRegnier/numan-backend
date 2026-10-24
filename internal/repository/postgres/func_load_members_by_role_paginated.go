package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadMembersByRolePaginated charge les membres d'une conversation par rôle (ex: -3 pour les requêtes)
func FuncLoadMembersByRolePaginated(ctx context.Context, convID int64, role int, limit int64, offset int64) ([]member_models.MemberPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT * FROM messaging.func_load_members_by_role_paginated($1, $2, $3, $4)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, convID, role, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les membres par rôle.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	members := make([]member_models.MemberPayload, 0)
	for rows.Next() {
		var mem member_models.MemberPayload
		var frozenDB sql.NullInt64 // Gestion du potentiel NULL
		var lastDB sql.NullInt64

		err := rows.Scan(
			&mem.ID, &mem.ConversationID, &mem.UserID, &mem.Role,
			&mem.JoinedAt, &mem.UnreadCount, &frozenDB, &lastDB, &mem.CreatedAt, &mem.UpdatedAt,
		)
		if err == nil {
			if frozenDB.Valid {
				mem.FrozenMessageID = frozenDB.Int64
			}
			if lastDB.Valid {
				mem.LastReadMessageID = lastDB.Int64
			}
			members = append(members, mem)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les membres par rôle.",
			err,
		)
	}

	return members, nil
}
