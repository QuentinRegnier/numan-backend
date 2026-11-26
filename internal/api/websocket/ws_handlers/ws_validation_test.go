package ws_handlers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/websocket/ws_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertWSBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestWSHandlers_PayloadValidation(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	initialKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)

	_, err = ws_handlers.HandleReadReceipt(ctx, 1, []byte(`{"conversation_id":0}`))
	assertWSBadRequest(t, err)

	_, err = ws_handlers.HandleCreateMessage(ctx, 1, []byte(`{"conversation_id":1,"message_type":0,"content":"   "}`))
	assertWSBadRequest(t, err)

	// HandleUpdateMessage must return *numan_error.AppError (not raw errors.New) on invalid JSON or payload
	_, err = ws_handlers.HandleUpdateMessage(ctx, 1, []byte(`{invalid`))
	assertWSBadRequest(t, err)
	_, err = ws_handlers.HandleUpdateMessage(ctx, 1, []byte(`{"message_id":0,"content":"hi"}`))
	assertWSBadRequest(t, err)

	_, err = ws_handlers.HandleDeleteMessage(ctx, 1, []byte(`{"message_id":0}`))
	assertWSBadRequest(t, err)

	err = ws_handlers.HandleReactMessage(ctx, 1, []byte(`{"message_id":1,"reaction":"  "}`))
	assertWSBadRequest(t, err)

	err = ws_handlers.HandleUnreactMessage(ctx, 1, []byte(`{"message_id":0}`))
	assertWSBadRequest(t, err)

	_, err = ws_handlers.HandleSyncPresence(ctx, []byte(`{"user_ids":[]}`))
	assertWSBadRequest(t, err)
	_, err = ws_handlers.HandleSyncPresence(ctx, []byte(`{"user_ids":[1,-2]}`))
	assertWSBadRequest(t, err)

	err = ws_handlers.HandleFocusConversation(ctx, 1, []byte(`{"conversation_id":0,"active":true}`))
	assertWSBadRequest(t, err)

	err = ws_handlers.HandleTyping(ctx, 1, []byte(`{"conversation_id":0}`), true)
	assertWSBadRequest(t, err)

	finalKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)
}
