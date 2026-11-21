package variables_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanguageConstantsAndHelpers(t *testing.T) {
	t.Run("IsSupportedLanguage and IsValidPostLanguage", func(t *testing.T) {
		for _, code := range variables.SupportedLanguageCodes {
			assert.True(t, variables.IsSupportedLanguage(code))
			assert.True(t, variables.IsValidPostLanguage(code))
		}
		assert.False(t, variables.IsSupportedLanguage(variables.LangLegacyUnknown))
		assert.True(t, variables.IsValidPostLanguage(variables.LangLegacyUnknown))
		assert.False(t, variables.IsSupportedLanguage(999))
		assert.False(t, variables.IsValidPostLanguage(999))
		assert.False(t, variables.IsValidPostLanguage(-2))
	})

	t.Run("LangToISO and ParseLanguageISO round-trip", func(t *testing.T) {
		assert.Equal(t, "fr", variables.LangToISO(variables.LangFR))
		assert.Equal(t, "en", variables.LangToISO(variables.LangEN))
		assert.Equal(t, "es", variables.LangToISO(variables.LangES))
		assert.Equal(t, "un", variables.LangToISO(variables.LangLegacyUnknown))
		assert.Equal(t, "fr", variables.LangToISO(999))

		code, err := variables.ParseLanguageISO("EN-US")
		require.NoError(t, err)
		assert.Equal(t, variables.LangEN, code)

		code, err = variables.ParseLanguageISO("fr_FR")
		require.NoError(t, err)
		assert.Equal(t, variables.LangFR, code)

		_, err = variables.ParseLanguageISO("xx")
		assert.Error(t, err)
	})

	t.Run("SnowballLanguageForCode", func(t *testing.T) {
		assert.Equal(t, "french", variables.SnowballLanguageForCode(variables.LangFR))
		assert.Equal(t, "english", variables.SnowballLanguageForCode(variables.LangEN))
		assert.Equal(t, "spanish", variables.SnowballLanguageForCode(variables.LangES))
		assert.Equal(t, "", variables.SnowballLanguageForCode(variables.LangAR))
	})

	t.Run("ResolveContentLanguage precedence", func(t *testing.T) {
		en := variables.LangEN
		resolved, err := variables.ResolveContentLanguage(&en, "", variables.LangFR)
		require.NoError(t, err)
		assert.Equal(t, variables.LangEN, resolved)

		invalid := 99
		_, err = variables.ResolveContentLanguage(&invalid, "", variables.LangFR)
		assert.Error(t, err)

		resolved, err = variables.ResolveContentLanguage(nil, "es", variables.LangFR)
		require.NoError(t, err)
		assert.Equal(t, variables.LangES, resolved)

		_, err = variables.ResolveContentLanguage(nil, "invalid_iso", variables.LangFR)
		assert.Error(t, err)

		resolved, err = variables.ResolveContentLanguage(nil, "", variables.LangIT)
		require.NoError(t, err)
		assert.Equal(t, variables.LangIT, resolved)

		resolved, err = variables.ResolveContentLanguage(nil, "", -1)
		require.NoError(t, err)
		assert.Equal(t, variables.DefaultContentLanguage, resolved)
	})

	t.Run("GetLanguageFallbackChain", func(t *testing.T) {
		assert.Equal(t, []int{variables.LangFR, variables.LangEN}, variables.GetLanguageFallbackChain(variables.LangFR))
		assert.Equal(t, []int{variables.LangEN, variables.LangFR}, variables.GetLanguageFallbackChain(variables.LangEN))
		assert.Equal(t, []int{variables.LangES, variables.LangFR, variables.LangEN}, variables.GetLanguageFallbackChain(variables.LangES))
	})
}
