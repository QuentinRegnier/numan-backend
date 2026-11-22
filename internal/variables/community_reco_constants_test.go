package variables

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommunityRecoVariables_All18RegisteredWithDefaults(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	_, details, _ := GetSnapshot()

	expectedKeys := []string{
		"CommunityRecoWeightMembers",
		"CommunityRecoWeightDynamism",
		"CommunityRecoWeightGrowth",
		"CommunityRecoTrustAlphaOwner",
		"CommunityRecoTrustAlphaAdmin",
		"CommunityRecoTrustAlphaMember",
		"CommunityRecoTrustPriorMean",
		"CommunityRecoTrustPriorStrength",
		"CommunityRecoTrustMemberSampleCap",
		"CommunityRecoModeMultFreeChat",
		"CommunityRecoModeMultSubreddit",
		"CommunityRecoModeMultAnnouncement",
		"CommunityRecoPartnerMultiplier",
		"CommunityRecoFreshnessBoost",
		"CommunityRecoFreshnessDays",
		"CommunityRecoFriendBoostGamma",
		"CommunityRecoFriendEvalCap",
		"CommunityRecoCronInterval",
	}

	for _, key := range expectedKeys {
		info, ok := details[key]
		require.True(t, ok, "variable %s must be registered in dynamic registry", key)
		assert.Equal(t, "community_recommendation", info.Category)
	}

	assert.Equal(t, 0.45, CommunityRecoTrustAlphaOwner)
	assert.Equal(t, 0.30, CommunityRecoTrustAlphaAdmin)
	assert.Equal(t, 0.25, CommunityRecoTrustAlphaMember)
	assert.Equal(t, 0.50, CommunityRecoTrustPriorMean)
	assert.Equal(t, 10.0, CommunityRecoTrustPriorStrength)
	assert.Equal(t, 200, CommunityRecoTrustMemberSampleCap)
	assert.Equal(t, 1.15, CommunityRecoModeMultFreeChat)
	assert.Equal(t, 1.15, CommunityRecoModeMultSubreddit)
	assert.Equal(t, 0.45, CommunityRecoModeMultAnnouncement)
	assert.Equal(t, 1.25, CommunityRecoPartnerMultiplier)
	assert.Equal(t, 0.30, CommunityRecoFreshnessBoost)
	assert.Equal(t, 14, CommunityRecoFreshnessDays)
	assert.Equal(t, 0.25, CommunityRecoFriendBoostGamma)
	assert.Equal(t, 500, CommunityRecoFriendEvalCap)
	assert.Equal(t, 2*time.Hour, CommunityRecoCronInterval)
}

func TestCommunityRecoVariables_HotReloadValidation(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	t.Run("accepts valid patch and resets cleanly", func(t *testing.T) {
		patch, err := ValidateAndPreparePatch(map[string]json.RawMessage{
			"CommunityRecoFriendBoostGamma":     json.RawMessage(`0.4`),
			"CommunityRecoFriendEvalCap":        json.RawMessage(`300`),
			"CommunityRecoPartnerMultiplier":    json.RawMessage(`1.5`),
			"CommunityRecoCronInterval":         json.RawMessage(`"1h30m"`),
			"CommunityRecoTrustMemberSampleCap": json.RawMessage(`150`),
		})
		require.NoError(t, err)
		ApplyPreparedPatchInMemory(patch, GetVersion()+1)
		t.Cleanup(resetAllVariablesForTest)

		assert.Equal(t, 0.4, CommunityRecoFriendBoostGamma)
		assert.Equal(t, 300, CommunityRecoFriendEvalCap)
		assert.Equal(t, 1.5, CommunityRecoPartnerMultiplier)
		assert.Equal(t, 90*time.Minute, CommunityRecoCronInterval)
		assert.Equal(t, 150, CommunityRecoTrustMemberSampleCap)
	})

	invalidCases := []struct {
		name  string
		patch map[string]json.RawMessage
	}{
		{
			name:  "negative member weight",
			patch: map[string]json.RawMessage{"CommunityRecoWeightMembers": json.RawMessage(`-0.1`)},
		},
		{
			name:  "zero partner multiplier",
			patch: map[string]json.RawMessage{"CommunityRecoPartnerMultiplier": json.RawMessage(`0`)},
		},
		{
			name:  "excessive partner multiplier",
			patch: map[string]json.RawMessage{"CommunityRecoPartnerMultiplier": json.RawMessage(`50`)},
		},
		{
			name:  "alpha owner above 1",
			patch: map[string]json.RawMessage{"CommunityRecoTrustAlphaOwner": json.RawMessage(`1.5`)},
		},
		{
			name:  "zero friend eval cap",
			patch: map[string]json.RawMessage{"CommunityRecoFriendEvalCap": json.RawMessage(`0`)},
		},
		{
			name:  "excessive friend eval cap",
			patch: map[string]json.RawMessage{"CommunityRecoFriendEvalCap": json.RawMessage(`99999`)},
		},
		{
			name:  "cron interval too short",
			patch: map[string]json.RawMessage{"CommunityRecoCronInterval": json.RawMessage(`"5s"`)},
		},
		{
			name:  "zero freshness days",
			patch: map[string]json.RawMessage{"CommunityRecoFreshnessDays": json.RawMessage(`0`)},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateAndPreparePatch(tc.patch)
			require.Error(t, err)
		})
	}
}

func TestNormalizeCommunityLang(t *testing.T) {
	isos := SupportedLanguageISOs()
	assert.Contains(t, isos, "fr")
	assert.Contains(t, isos, "en")
	assert.Len(t, isos, len(SupportedLanguageCodes))

	validCases := map[string]string{
		"fr":   "fr",
		" FR ": "fr",
		"En":   "en",
		"es":   "es",
		"it":   "it",
		"pt":   "pt",
		"de":   "de",
		"nl":   "nl",
		"ru":   "ru",
		"ar":   "ar",
	}
	for input, want := range validCases {
		got, ok := NormalizeCommunityLang(input)
		assert.True(t, ok, "expected %q to be valid", input)
		assert.Equal(t, want, got)
		assert.True(t, IsSupportedCommunityLang(input))
	}

	invalidCases := []string{"", "   ", "un", "xx", "jp", "fr-FR", "french", "123456789"}
	for _, input := range invalidCases {
		got, ok := NormalizeCommunityLang(input)
		assert.False(t, ok, "expected %q to be invalid", input)
		assert.Empty(t, got)
		assert.False(t, IsSupportedCommunityLang(input))
	}
}
