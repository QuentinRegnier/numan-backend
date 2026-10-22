package postgres

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

type postgresTarget struct {
	Schema string
	Table  string
}

// redis2Postgres fait le pont entre le domaine (Redis EntityType) et le schéma physique SQL.
func redis2Postgres(ctx context.Context, entity redis.EntityType) (postgresTarget, error) {
	switch entity {
	case redis.EntityUser:
		return postgresTarget{Schema: "auth", Table: "users"}, nil
	case redis.EntityUserSettings:
		return postgresTarget{Schema: "auth", Table: "user_settings"}, nil
	case redis.EntitySession:
		return postgresTarget{Schema: "auth", Table: "sessions"}, nil
	case redis.EntityRelation:
		return postgresTarget{Schema: "auth", Table: "relations"}, nil
	case redis.EntityPost:
		return postgresTarget{Schema: "content", Table: "posts"}, nil
	case redis.EntityComment:
		return postgresTarget{Schema: "content", Table: "comments"}, nil
	case redis.EntityLike:
		return postgresTarget{Schema: "content", Table: "likes"}, nil
	case redis.EntityMedia:
		return postgresTarget{Schema: "content", Table: "media"}, nil
	case redis.EntityConversation:
		return postgresTarget{Schema: "messaging", Table: "conversations"}, nil
	case redis.EntityMembers:
		return postgresTarget{Schema: "messaging", Table: "members"}, nil
	case redis.EntityMessage:
		return postgresTarget{Schema: "messaging", Table: "messages"}, nil
	case redis.EntityPasskey:
		return postgresTarget{Schema: "auth", Table: "user_passkeys"}, nil
	default:
		return postgresTarget{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Entité non supportée.",
			fmt.Errorf("unsupported entity type for PostgreSQL resolution: %s", entity),
		)
	}
}
