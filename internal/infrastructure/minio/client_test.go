package minio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func clearMinioEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MINIO_ENDPOINT",
		"S3_ACCESS_KEY", "S3_SECRET_KEY",
		"MINIO_ACCESS_KEY", "MINIO_SECRET_KEY",
		"MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD",
		"MINIO_BUCKET_NAME",
		"S3_USE_SSL", "MINIO_USE_SSL", "USE_SSL",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveMinioConfig_PrefersS3LeastPrivilegeKeys(t *testing.T) {
	clearMinioEnv(t)
	t.Setenv("MINIO_ENDPOINT", "minio-hot:9000")
	t.Setenv("S3_ACCESS_KEY", "app-access")
	t.Setenv("S3_SECRET_KEY", "app-secret")
	t.Setenv("MINIO_ACCESS_KEY", "wm-access")
	t.Setenv("MINIO_SECRET_KEY", "wm-secret")
	t.Setenv("MINIO_ROOT_USER", "root-user")
	t.Setenv("MINIO_ROOT_PASSWORD", "root-pass")
	t.Setenv("MINIO_BUCKET_NAME", "custom-bucket")
	t.Setenv("S3_USE_SSL", "true")

	cfg := ResolveMinioConfig()
	assert.Equal(t, "minio-hot:9000", cfg.Endpoint)
	assert.Equal(t, "app-access", cfg.AccessKeyID)
	assert.Equal(t, "app-secret", cfg.SecretAccessKey)
	assert.Equal(t, "custom-bucket", cfg.BucketName)
	assert.True(t, cfg.UseSSL)
}

func TestResolveMinioConfig_FallbacksToMinioAccessThenRoot(t *testing.T) {
	clearMinioEnv(t)
	t.Setenv("MINIO_ACCESS_KEY", "wm-access")
	t.Setenv("MINIO_SECRET_KEY", "wm-secret")
	t.Setenv("MINIO_ROOT_USER", "root-user")
	t.Setenv("MINIO_ROOT_PASSWORD", "root-pass")

	cfg := ResolveMinioConfig()
	assert.Equal(t, "wm-access", cfg.AccessKeyID)
	assert.Equal(t, "wm-secret", cfg.SecretAccessKey)
	assert.Equal(t, "numan-bucket", cfg.BucketName)
	assert.False(t, cfg.UseSSL)

	clearMinioEnv(t)
	t.Setenv("MINIO_ROOT_USER", "root-user")
	t.Setenv("MINIO_ROOT_PASSWORD", "root-pass")

	cfgRoot := ResolveMinioConfig()
	assert.Equal(t, "root-user", cfgRoot.AccessKeyID)
	assert.Equal(t, "root-pass", cfgRoot.SecretAccessKey)
}
