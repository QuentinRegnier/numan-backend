package report_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReportInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilInput *report_models.CreateReportInput
	assertBadRequest(t, nilInput.Validate())

	validPost := report_models.CreateReportInput{
		TargetType: variables.ReportTargetPost, // 0
		TargetIDs:  []int64{101},
		Category:   variables.ReportCatSpam,
		Reason:     "Spam évident",
	}
	assert.NoError(t, validPost.Validate())

	validOtherCat := validPost
	validOtherCat.TargetType = variables.ReportTargetUser
	validOtherCat.Category = variables.ReportCatOther
	assert.NoError(t, validOtherCat.Validate())

	cases := []struct {
		name   string
		mutate func(*report_models.CreateReportInput)
	}{
		{"invalid_target_type_4", func(p *report_models.CreateReportInput) { p.TargetType = 4 }},
		{"negative_target_type", func(p *report_models.CreateReportInput) { p.TargetType = -1 }},
		{"empty_target_ids", func(p *report_models.CreateReportInput) { p.TargetIDs = nil }},
		{"non_positive_target_id", func(p *report_models.CreateReportInput) { p.TargetIDs = []int64{10, 0} }},
		{"too_many_targets", func(p *report_models.CreateReportInput) {
			p.TargetIDs = make([]int64, 21)
			for i := range p.TargetIDs {
				p.TargetIDs[i] = int64(i + 1)
			}
		}},
		{"invalid_category_0", func(p *report_models.CreateReportInput) { p.Category = 0 }},
		{"invalid_category_11", func(p *report_models.CreateReportInput) { p.Category = 11 }},
		{"reason_too_long", func(p *report_models.CreateReportInput) { p.Reason = strings.Repeat("r", 1001) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPost
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
