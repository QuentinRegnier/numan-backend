package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// redis2Mongo fait le pont entre le domaine (Redis EntityType) et les collections MongoDB.
func redis2Mongo(ctx context.Context, entity redis.EntityType) (*Collection, error) {
	var coll *Collection

	switch entity {
	case redis.EntityUser:
		coll = Users
	case redis.EntityUserSettings:
		coll = UserSettings
	case redis.EntitySession:
		coll = Sessions
	case redis.EntityRelation:
		coll = Relations
	case redis.EntityPost:
		coll = Posts
	case redis.EntityComment:
		coll = Comments
	case redis.EntityLike:
		coll = Likes
	case redis.EntityMedia:
		coll = Media
	case redis.EntityConversation:
		coll = Conversations
	case redis.EntityMembers:
		coll = Members
	case redis.EntityMessage:
		coll = Messages
	case redis.EntityMessageReaction:
		coll = MessageReactions
	case redis.EntitySaved:
		coll = Saved
	case redis.EntityPasskey:
		coll = Passkeys
	case redis.EntityNotification:
		coll = Notifications
	default:
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Entité non supportée.",
			fmt.Errorf("unsupported entity type for MongoDB resolution: %s", entity),
		)
	}

	if coll == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection is not initialized"),
		)
	}

	return coll, nil
}
