package security_handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/security_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshMasterHandler_Validation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name         string
		body         string
		expectedCode string
	}{
		{
			name:         "negative user_id returns CodeInvalidPayload",
			body:         `{"id_user": -1, "master_token": "tok", "username": "alice"}`,
			expectedCode: numan_error.CodeInvalidPayload,
		},
		{
			name:         "blank master_token returns CodeInvalidPayload",
			body:         `{"id_user": 1, "master_token": "   ", "username": "alice"}`,
			expectedCode: numan_error.CodeInvalidPayload,
		},
		{
			name:         "blank username returns CodeInvalidPayload",
			body:         `{"id_user": 1, "master_token": "tok", "username": ""}`,
			expectedCode: numan_error.CodeInvalidPayload,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/refresh/master", bytes.NewBufferString(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")

			security_handlers.RefreshMaster(c)
			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp numan_error.PublicErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tc.expectedCode, resp.Code)
		})
	}
}
