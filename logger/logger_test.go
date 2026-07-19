package logger

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadMetadataDoesNotExposePayload(t *testing.T) {
	payload := []byte(`{"prompt":"private prompt","token":"secret-token"}`)
	digest := sha256.Sum256(payload)

	metadata := PayloadMetadata(payload)

	assert.Equal(t, fmt.Sprintf("size=%d sha256=%x", len(payload), digest), metadata)
	assert.NotContains(t, metadata, "private prompt")
	assert.NotContains(t, metadata, "secret-token")
}

func TestLogJsonWritesOnlyPayloadMetadata(t *testing.T) {
	originalDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() { common.DebugEnabled = originalDebug })

	var output bytes.Buffer
	common.LogWriterMu.Lock()
	originalErrorWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = originalErrorWriter
		common.LogWriterMu.Unlock()
	})

	LogJson(nil, "converted request", map[string]string{"prompt": "do-not-log-this"})

	logged := output.String()
	assert.Contains(t, logged, "converted request")
	assert.Contains(t, logged, "size=")
	assert.Contains(t, logged, "sha256=")
	assert.NotContains(t, logged, "do-not-log-this")
}

func TestRotatingFileWriterEnforcesBoundsAndPermissions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0755))

	stalePath := filepath.Join(dir, "oneapi-stale.log")
	require.NoError(t, os.WriteFile(stalePath, []byte("stale"), 0644))
	staleTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stalePath, staleTime, staleTime))
	legacyPath := filepath.Join(dir, "oneapi-legacy.log")
	require.NoError(t, os.WriteFile(legacyPath, []byte("legacy"), 0644))

	writer, err := newRotatingFileWriter(dir, 64, time.Hour, 2)
	require.NoError(t, err)
	legacyInfo, err := os.Stat(legacyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), legacyInfo.Mode().Perm())
	require.NoError(t, func() error {
		_, writeErr := writer.Write(bytes.Repeat([]byte("x"), 256))
		return writeErr
	}())
	require.NoError(t, writer.Close())

	_, err = os.Stat(stalePath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	archiveCount := 0
	for _, entry := range entries {
		info, infoErr := entry.Info()
		require.NoError(t, infoErr)
		assert.LessOrEqual(t, info.Size(), int64(64), entry.Name())
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), entry.Name())
		if entry.Name() != activeLogFileName {
			archiveCount++
		}
	}
	assert.LessOrEqual(t, archiveCount, 2)
}

func TestRotatingFileWriterSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRotatingFileWriter(dir, 512, time.Hour, 64)
	require.NoError(t, err)

	const (
		goroutines    = 24
		writesPerLoop = 40
		writeSize     = 32
	)
	var wg sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		wg.Add(1)
		go func(fill byte) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{fill}, writeSize)
			for write := 0; write < writesPerLoop; write++ {
				n, writeErr := writer.Write(payload)
				assert.NoError(t, writeErr)
				assert.Equal(t, len(payload), n)
			}
		}(byte('a' + worker%26))
	}
	wg.Wait()
	require.NoError(t, writer.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var totalSize int64
	for _, entry := range entries {
		info, infoErr := entry.Info()
		require.NoError(t, infoErr)
		assert.LessOrEqual(t, info.Size(), int64(512), entry.Name())
		totalSize += info.Size()
	}
	assert.Equal(t, int64(goroutines*writesPerLoop*writeSize), totalSize)
}
