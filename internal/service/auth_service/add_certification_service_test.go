package auth_service_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

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
	now := time.Now()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		// Mock pour postgres.FuncLoadUser
		return test.NewMockRows(
			[]string{"id", "username", "email", "password_hash", "first_name", "last_name", "grade", "badges", "banned", "desactivated", "trust_score", "created_at", "updated_at"},
			[][]driver.Value{{userID, "alice", "alice@example.com", "hash", "Alice", "Dupont", 0, "{}", false, false, 0.40, now, now}},
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
