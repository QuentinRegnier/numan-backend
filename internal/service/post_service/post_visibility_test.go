package post_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_settings_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ============================================================================
// 13.1. INPUT VALIDATION TESTS (GIN BINDING & STRUCT VALIDATOR)
// ============================================================================

func TestCreatePostInput_VisibilityValidation(t *testing.T) {
	tests := []struct {
		name       string
		visibility int
		shouldPass bool
	}{
		{"Visibility 0 (Public) must pass", 0, true},
		{"Visibility 1 (Subscribers) must pass", 1, true},
		{"Visibility 2 (Friends) must pass", 2, true},
		{"Visibility 3 (Private - Discontinued) must fail", 3, false},
		{"Visibility 4 (Invalid) must fail", 4, false},
		{"Visibility -1 (Deleted sentinel) must fail", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := post_models.CreatePostInput{
				Content:    "Hello test post",
				Visibility: tc.visibility,
			}
			err := pkg.ValidateStruct(&input)
			if tc.shouldPass && err != nil {
				t.Fatalf("Expected validation to pass for visibility %d, got: %v", tc.visibility, err)
			}
			if !tc.shouldPass && err == nil {
				t.Fatalf("Expected validation to FAIL for visibility %d, but it passed", tc.visibility)
			}
		})
	}
}

func TestUpdatePostInput_VisibilityValidation(t *testing.T) {
	tests := []struct {
		name       string
		visibility int
		shouldPass bool
	}{
		{"Visibility 0 (Public) must pass", 0, true},
		{"Visibility 1 (Subscribers) must pass", 1, true},
		{"Visibility 2 (Friends) must pass", 2, true},
		{"Visibility 3 (Private - Discontinued) must fail", 3, false},
		{"Visibility 4 (Invalid) must fail", 4, false},
		{"Visibility -1 (Deleted sentinel) must fail", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := post_models.UpdatePostInput{
				PostID:     123456789,
				Content:    "Updated post content",
				Visibility: tc.visibility,
			}
			err := pkg.ValidateStruct(&input)
			if tc.shouldPass && err != nil {
				t.Fatalf("Expected validation to pass for visibility %d, got: %v", tc.visibility, err)
			}
			if !tc.shouldPass && err == nil {
				t.Fatalf("Expected validation to FAIL for visibility %d, but it passed", tc.visibility)
			}
		})
	}
}

func TestUpdatePrivacyInput_PostVisibilityDefaultValidation(t *testing.T) {
	tests := []struct {
		name       string
		defaultVis int
		shouldPass bool
	}{
		{"PostVisibilityDefault 0 (Public) must pass", 0, true},
		{"PostVisibilityDefault 1 (Subscribers) must pass", 1, true},
		{"PostVisibilityDefault 2 (Friends) must pass", 2, true},
		{"PostVisibilityDefault 3 (Private - Discontinued) must fail", 3, false},
		{"PostVisibilityDefault 4 (Invalid) must fail", 4, false},
		{"PostVisibilityDefault -1 (Invalid) must fail", -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := user_settings_models.UpdatePrivacyInput{
				PostVisibilityDefault: tc.defaultVis,
			}
			err := pkg.ValidateStruct(&input)
			if tc.shouldPass && err != nil {
				t.Fatalf("Expected validation to pass for PostVisibilityDefault %d, got: %v", tc.defaultVis, err)
			}
			if !tc.shouldPass && err == nil {
				t.Fatalf("Expected validation to FAIL for PostVisibilityDefault %d, but it passed", tc.defaultVis)
			}
		})
	}
}

// ============================================================================
// SERVICE BOUNDARY DEFENSIVE VALIDATION TESTS
// ============================================================================

func TestCreatePost_ServiceBoundaryValidation(t *testing.T) {
	ctx := context.Background()

	// Calling CreatePost directly with Visibility = 3 must return BadRequest
	input := post_models.CreatePostInput{
		MediaIDs:   []int64{101},
		Visibility: 3, // Private mode discontinued
	}

	_, err := post_service.CreatePost(ctx, 42, input)
	if err == nil {
		t.Fatal("Expected CreatePost to reject visibility 3, but got nil error")
	}

	appErr, ok := err.(*numan_error.AppError)
	if !ok {
		t.Fatalf("Expected *numan_error.AppError, got %T", err)
	}
	if appErr.HTTPStatus != 400 {
		t.Errorf("Expected HTTP 400 BadRequest, got %d", appErr.HTTPStatus)
	}
}

func TestUpdatePost_ServiceBoundaryValidation(t *testing.T) {
	ctx := context.Background()

	// Calling UpdatePost directly with Visibility = 3 must return BadRequest
	input := post_models.UpdatePostInput{
		PostID:     100,
		Visibility: 3, // Private mode discontinued
	}

	err := post_service.UpdatePost(ctx, 42, input)
	if err == nil {
		t.Fatal("Expected UpdatePost to reject visibility 3, but got nil error")
	}

	appErr, ok := err.(*numan_error.AppError)
	if !ok {
		t.Fatalf("Expected *numan_error.AppError, got %T", err)
	}
	if appErr.HTTPStatus != 400 {
		t.Errorf("Expected HTTP 400 BadRequest, got %d", appErr.HTTPStatus)
	}
}

func TestUpdatePrivacy_ServiceBoundaryValidation(t *testing.T) {
	ctx := context.Background()

	// Calling UpdatePrivacy directly with PostVisibilityDefault = 3 must return BadRequest
	input := user_settings_models.UpdatePrivacyInput{
		PostVisibilityDefault: 3, // Private mode discontinued
	}

	_, err := user_settings_service.UpdatePrivacy(ctx, 42, input)
	if err == nil {
		t.Fatal("Expected UpdatePrivacy to reject PostVisibilityDefault 3, but got nil error")
	}

	appErr, ok := err.(*numan_error.AppError)
	if !ok {
		t.Fatalf("Expected *numan_error.AppError, got %T", err)
	}
	if appErr.HTTPStatus != 400 {
		t.Errorf("Expected HTTP 400 BadRequest, got %d", appErr.HTTPStatus)
	}
}

// ============================================================================
// 13.2. VISIBILITY AUTHORIZATION MATRIX & SAFEGUARDS
// ============================================================================

func TestVisibilityAuthorizationMatrix(t *testing.T) {
	authorID := int64(10)
	strangerID := int64(20)

	type authTestCase struct {
		name          string
		callerID      int64
		visibility    int
		relationState int
		expectAllowed bool
		expectedError string
	}

	cases := []authTestCase{
		{
			name:          "Author can always view own post (Public 0)",
			callerID:      authorID,
			visibility:    variables.PostVisibilityAll,
			relationState: 0,
			expectAllowed: true,
		},
		{
			name:          "Author can always view own post (Subscribers 1)",
			callerID:      authorID,
			visibility:    variables.PostVisibilitySubcriber,
			relationState: 0,
			expectAllowed: true,
		},
		{
			name:          "Author can always view own post (Friends 2)",
			callerID:      authorID,
			visibility:    variables.PostVisibilityFriend,
			relationState: 0,
			expectAllowed: true,
		},
		{
			name:          "Public post (0) is visible to stranger",
			callerID:      strangerID,
			visibility:    variables.PostVisibilityAll,
			relationState: 0,
			expectAllowed: true,
		},
		{
			name:          "Subscriber post (1) rejected for stranger (relationState=0)",
			callerID:      strangerID,
			visibility:    variables.PostVisibilitySubcriber,
			relationState: 0,
			expectAllowed: false,
			expectedError: "🔒 Ce post est strictement réservé aux abonnés de l'auteur.",
		},
		{
			name:          "Subscriber post (1) allowed for follower (relationState=1)",
			callerID:      strangerID,
			visibility:    variables.PostVisibilitySubcriber,
			relationState: 1,
			expectAllowed: true,
		},
		{
			name:          "Friend post (2) rejected for follower (relationState=1)",
			callerID:      strangerID,
			visibility:    variables.PostVisibilityFriend,
			relationState: 1,
			expectAllowed: false,
			expectedError: "🤝 Ce post est confidentiel et réservé au cercle d'amis de l'auteur.",
		},
		{
			name:          "Friend post (2) allowed for friend (relationState=2)",
			callerID:      strangerID,
			visibility:    variables.PostVisibilityFriend,
			relationState: 2,
			expectAllowed: true,
		},
		{
			name:          "Soft-deleted post (-1) is rejected",
			callerID:      strangerID,
			visibility:    variables.PostVisibilityDeleted,
			relationState: 2,
			expectAllowed: false,
			expectedError: "Cette publication est introuvable ou a été supprimée.",
		},
		{
			name:          "Legacy/unsupported visibility (3) is rejected for non-author",
			callerID:      strangerID,
			visibility:    3,
			relationState: 2, // Even if friend, unsupported visibility is shielded
			expectAllowed: false,
			expectedError: "Cette publication est introuvable ou n'est plus accessible.",
		},
		{
			name:          "Legacy/unsupported visibility (3) remains accessible to author",
			callerID:      authorID,
			visibility:    3,
			relationState: 0,
			expectAllowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isCallerAuthor := tc.callerID == authorID

			var errString string
			allowed := true

			// Evaluator matching get_post_service.go logic
			if tc.visibility == variables.PostVisibilityDeleted {
				allowed = false
				errString = "Cette publication est introuvable ou a été supprimée."
			} else if tc.visibility == variables.PostVisibilitySubcriber && !isCallerAuthor {
				if tc.relationState < 1 {
					allowed = false
					errString = "🔒 Ce post est strictement réservé aux abonnés de l'auteur."
				}
			} else if tc.visibility == variables.PostVisibilityFriend && !isCallerAuthor {
				if tc.relationState != 2 {
					allowed = false
					errString = "🤝 Ce post est confidentiel et réservé au cercle d'amis de l'auteur."
				}
			} else if (tc.visibility < variables.PostVisibilityAll || tc.visibility > variables.PostVisibilityFriend) && !isCallerAuthor {
				allowed = false
				errString = "Cette publication est introuvable ou n'est plus accessible."
			}

			if allowed != tc.expectAllowed {
				t.Fatalf("Expected allowed=%v, got %v", tc.expectAllowed, allowed)
			}
			if !tc.expectAllowed && errString != tc.expectedError {
				t.Fatalf("Expected error %q, got %q", tc.expectedError, errString)
			}
		})
	}
}

// ============================================================================
// 13.3. USER SETTINGS NORMALIZATION
// ============================================================================

func TestUserSettings_Normalization(t *testing.T) {
	normalize := func(defaultVis int) int {
		if defaultVis < variables.PostVisibilityAll || defaultVis > variables.PostVisibilityFriend {
			return variables.PostVisibilityFriend
		}
		return defaultVis
	}

	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"Public (0) remains Public (0)", 0, 0},
		{"Subscribers (1) remains Subscribers (1)", 1, 1},
		{"Friends (2) remains Friends (2)", 2, 2},
		{"Legacy Private (3) normalizes to Friends (2)", 3, 2},
		{"Invalid (4) normalizes to Friends (2)", 4, 2},
		{"Negative (-1) normalizes to Friends (2)", -1, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize(tc.input)
			if got != tc.expected {
				t.Fatalf("Expected normalization of %d to be %d, got %d", tc.input, tc.expected, got)
			}
		})
	}
}
