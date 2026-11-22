package logger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitLogger_StdoutOnlyInReleaseOrWhenDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "should-not-exist.log")

	t.Setenv("LOG_TO_FILE", "false")
	t.Setenv("LOG_FILE_PATH", logFile)

	InitLogger(context.Background())
	Log.Info().Msg("stdout only test")
	CloseLogger(context.Background())

	assert.False(t, hasAsyncWriter)
	_, err := os.Stat(logFile)
	assert.True(t, os.IsNotExist(err), "no log file should be created when LOG_TO_FILE=false")

	// Also verify default release mode disables disk logging
	t.Setenv("LOG_TO_FILE", "")
	t.Setenv("GIN_MODE", "release")
	assert.False(t, shouldLogToFile())
}

func TestInitLogger_FileEnabledWritesAndClosesCleanly(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "numan-test.log")

	t.Setenv("LOG_TO_FILE", "true")
	t.Setenv("LOG_FILE_PATH", logFile)

	InitLogger(context.Background())
	assert.True(t, hasAsyncWriter)
	Log.Info().Msg("file logger test message")
	CloseLogger(context.Background())
	assert.False(t, hasAsyncWriter)

	// Calling CloseLogger twice must be a safe no-op
	CloseLogger(context.Background())

	data, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "file logger test message")
}
