package models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/stretchr/testify/assert"
)

func TestResolveCommunityModeFromWritePermission(t *testing.T) {
	tests := []struct {
		name            string
		writePermission int
		want            models.CommunityMode
	}{
		{name: "free_chat (0)", writePermission: 0, want: models.CommunityModeFreeChat},
		{name: "announcement (1)", writePermission: 1, want: models.CommunityModeAnnouncement},
		{name: "subreddit (2)", writePermission: 2, want: models.CommunityModeSubreddit},
		{name: "unknown negative defaults to subreddit", writePermission: -1, want: models.CommunityModeSubreddit},
		{name: "unknown high defaults to subreddit", writePermission: 99, want: models.CommunityModeSubreddit},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := models.ResolveCommunityModeFromWritePermission(tc.writePermission)
			assert.Equal(t, tc.want, got)
			assert.True(t, got.IsValid())
		})
	}

	assert.False(t, models.CommunityMode("invalid").IsValid())
	assert.False(t, models.CommunityMode("").IsValid())
}
