package media_service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateWatermarkedURL_NormalizesPublicBaseURL(t *testing.T) {
	t.Setenv("WATERMARK_API_URL", "https://api.numan.app/ ")
	t.Setenv("WATERMARK_SECRET_KEY", "test-secret-key")

	url := generateWatermarkedURL("users/1/media/photo.avif", 1, 42, 99)
	assert.True(t, strings.HasPrefix(url, "https://api.numan.app/process?key=users/1/media/photo.avif&author=1&post=42&reader=99&ts="), "unexpected URL: %s", url)
	assert.NotContains(t, url, "//process")
	assert.Contains(t, url, "&sig=")
}
