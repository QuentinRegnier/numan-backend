package email_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/email_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestEmailPayload_Validate(t *testing.T) {
	var nilPayload *email_models.EmailPayload
	assertBadRequest(t, nilPayload.Validate())

	valid := email_models.EmailPayload{
		RecipientEmail: "alice@example.com",
		TemplateName:   "two_factor_code",
		Subject:        "Votre code de vérification",
		Data:           map[string]any{"Code": "123456"},
	}
	assert.NoError(t, valid.Validate())

	cases := []struct {
		name   string
		mutate func(*email_models.EmailPayload)
	}{
		{"empty_email", func(p *email_models.EmailPayload) { p.RecipientEmail = "   " }},
		{"invalid_email", func(p *email_models.EmailPayload) { p.RecipientEmail = "not-an-email" }},
		{"display_name_email_rejected", func(p *email_models.EmailPayload) { p.RecipientEmail = "Alice <alice@example.com>" }},
		{"email_header_injection", func(p *email_models.EmailPayload) { p.RecipientEmail = "alice@example.com\r\nBcc:evil@x.com" }},
		{"email_too_long", func(p *email_models.EmailPayload) {
			p.RecipientEmail = strings.Repeat("a", 250) + "@b.co"
		}},
		{"empty_template", func(p *email_models.EmailPayload) { p.TemplateName = "   " }},
		{"template_path_traversal_dotdot", func(p *email_models.EmailPayload) { p.TemplateName = "../secrets" }},
		{"template_path_slash", func(p *email_models.EmailPayload) { p.TemplateName = "dir/template" }},
		{"template_path_backslash", func(p *email_models.EmailPayload) { p.TemplateName = "dir\\template" }},
		{"template_too_long", func(p *email_models.EmailPayload) { p.TemplateName = strings.Repeat("t", 129) }},
		{"empty_subject", func(p *email_models.EmailPayload) { p.Subject = "   " }},
		{"subject_crlf_injection", func(p *email_models.EmailPayload) { p.Subject = "Hello\r\nBcc: attacker@example.com" }},
		{"subject_too_long", func(p *email_models.EmailPayload) { p.Subject = strings.Repeat("s", 256) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
