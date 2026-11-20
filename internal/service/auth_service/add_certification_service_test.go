package auth_service_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/auth_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCertification_PublishesVerifiedIdentityEvent(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	userID := int64(456789)
	now := domain.NowTime()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		// Mock pour postgres.FuncLoadUser (29 colonnes)
		return test.NewMockRows(
			[]string{
				"id", "username", "email", "email_verified", "phone", "phone_verified",
				"password_hash", "first_name", "last_name", "birthdate", "sex", "bio",
				"profile_picture_id", "grade", "location", "school", "work", "badges",
				"desactivated", "banned", "ban_reason", "ban_expires_at", "created_at", "updated_at",
				"two_factor_enabled", "two_factor_method", "totp_secret", "trust_score", "last_infraction_at",
			},
			[][]driver.Value{{
				userID, "alice", "alice@example.com", true, "+33123456789", true,
				"hash", "Alice", "Dupont", "2000-01-01T00:00:00Z", 1, "Bio",
				nil, 0, "Paris", nil, nil, "{}",
				false, false, nil, nil, now, now,
				false, 0, nil, 0.40, int64(0),
			}},
		), nil
	}, nil)

	out, err := auth_service.AddCertification(ctx, userID, auth_models.AddCertificationInput{
		Prestataire:     "federation_francaise",
		CertificationID: "LIC-12345",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.Badges)

	// Vérifie que l'événement de certification d'identité (+0.10) a été publié dans la file
	rawEvt, errPop := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errPop)
	assert.Contains(t, rawEvt, variables.TrustTriggerVerifiedIdentity)
	assert.Contains(t, rawEvt, "456789")
}
