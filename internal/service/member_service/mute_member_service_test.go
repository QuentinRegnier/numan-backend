package member_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/member_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMuteMember_PublishesMutedInPublicCommEvent(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	convID := int64(1001)
	adminCallerID := int64(2001)
	targetMemberID := int64(3001)

	// Conversation dans Object Cache
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conversation_models.ConversationPayload{
		ID:   convID,
		Type: variables.ConversationTypeGroup,
		Settings: conversation_models.ConversationSettings{
			WritePermission: 0,
		},
	}))

	// Membre Admin dans Object Cache
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             1,
		ConversationID: convID,
		UserID:         adminCallerID,
		Role:           variables.MemberRoleAdmin,
	}))

	// Membre Cible dans Object Cache
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             2,
		ConversationID: convID,
		UserID:         targetMemberID,
		Role:           variables.MemberRoleNormal,
	}))

	// Application d'un mute de 2 heures
	restrictedUntil := time.Now().Add(2 * time.Hour).UnixMilli()
	errMute := member_service.MuteMember(ctx, adminCallerID, member_models.MuteMemberInput{
		ConversationID:  convID,
		TargetUserID:    targetMemberID,
		RestrictedUntil: restrictedUntil,
	})
	require.NoError(t, errMute)

	// Vérifie que l'événement de mute dans un salon public (-0.10) a été publié dans la file
	rawEvt, errPop := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errPop)
	assert.Contains(t, rawEvt, variables.TrustTriggerMutedInPublicComm)
	assert.Contains(t, rawEvt, "3001")
}
