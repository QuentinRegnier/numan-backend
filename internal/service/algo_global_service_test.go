package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateRecommendationScore_QuarantineShadowban(t *testing.T) {
	baseOpts := ScoreOptions{
		LikesCount:       50,
		CommentsCount:    10,
		ViewCount:        500,
		MediaCount:       1,
		AuthorGrade:      1,
		AuthorTrustScore: 0.65, // Zone Standard
		AgeSeconds:       1800,
	}

	standardScore := CalculateRecommendationScore(baseOpts)
	assert.Greater(t, standardScore, 0.0)

	// Même post mais auteur en Zone de Quarantaine (TrustScore < 0.40) -> Score public nul (Shadowban)
	quarantineOpts := baseOpts
	quarantineOpts.AuthorTrustScore = 0.25
	assert.Equal(t, 0.0, CalculateRecommendationScore(quarantineOpts))
}
