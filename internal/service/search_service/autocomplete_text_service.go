package search_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AUTOCOMPLÉTION GLOBALE (UTILISATEURS & COMMUNAUTÉS)
// ############################################################################

func AutocompleteText(ctx context.Context, callerID int64, input search_models.AutocompleteTextInput) (search_models.AutocompleteTextOutput, error) {
	if err := input.Validate(); err != nil {
		return search_models.AutocompleteTextOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = variables.DefaultGlobalSearchLimit
		if input.Limit <= 0 {
			input.Limit = 10
		}
	}

	autocompleteOutput := search_models.AutocompleteTextOutput{
		Users:       search_models.UserSearchOutput{Users: make([]auth_models.UserLiteView, 0)},
		Communities: search_models.CommunitySearchOutput{Communities: make([]conversation_models.CommunityLiteView, 0)},
	}

	// ── ÉTAPE 1 : RECHERCHE DES UTILISATEURS (CACHE L1) ─────────────────────
	usersSearchInput := search_models.UserSearchInput{
		Prefix: input.Query,
		Limit:  input.Limit,
		Offset: input.Offset,
	}

	usersOutput, errUsers := searchUsers(ctx, callerID, usersSearchInput)
	if errUsers != nil {
		var appErr *numan_error.AppError
		if errors.As(errUsers, &appErr) && appErr.HTTPStatus < http.StatusInternalServerError {
			return search_models.AutocompleteTextOutput{}, errUsers
		}
		numan_log.Warn(ctx).Err(errUsers).Msg("AutocompleteText : échec partiel lors de la recherche d'utilisateurs")
	} else {
		autocompleteOutput.Users = usersOutput
	}

	// ── ÉTAPE 2 : RECHERCHE DES COMMUNAUTÉS (CACHE L1) ──────────────────────
	communitiesSearchInput := search_models.CommunitySearchInput{
		Prefix: input.Query,
		Limit:  input.Limit,
		Offset: input.Offset,
	}

	communitiesOutput, errCommunities := searchCommunities(ctx, callerID, communitiesSearchInput)
	if errCommunities != nil {
		var appErr *numan_error.AppError
		if errors.As(errCommunities, &appErr) && appErr.HTTPStatus < http.StatusInternalServerError {
			return search_models.AutocompleteTextOutput{}, errCommunities
		}
		numan_log.Warn(ctx).Err(errCommunities).Msg("AutocompleteText : échec partiel lors de la recherche de communautés")
	} else {
		autocompleteOutput.Communities = communitiesOutput
	}

	if errUsers != nil && errCommunities != nil {
		return search_models.AutocompleteTextOutput{}, errUsers
	}

	return autocompleteOutput, nil
}
