package common

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureBufferedResponseTest(t *testing.T, tempDir string, memoryBytes int, maxBytes int, totalBytes int, maxConcurrent int) {
	t.Helper()
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", tempDir)
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", fmt.Sprintf("%d", memoryBytes))
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", fmt.Sprintf("%d", maxBytes))
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", fmt.Sprintf("%d", totalBytes))
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", fmt.Sprintf("%d", maxConcurrent))
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")
}

func TestBufferedResponseWriterGinSemanticsAndCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Header("X-Before", "kept")

	buffer, err := NewBufferedResponseWriter(context.Writer)
	require.NoError(t, err)
	context.Writer = buffer

	assert.Equal(t, http.StatusOK, buffer.Status())
	assert.Equal(t, -1, buffer.Size())
	assert.False(t, buffer.Written())
	assert.Equal(t, "kept", buffer.Header().Get("X-Before"))

	buffer.Header().Set("X-Buffered", "yes")
	assert.Empty(t, recorder.Header().Get("X-Buffered"), "buffered headers must stay private")
	buffer.WriteHeader(http.StatusAccepted)
	assert.Equal(t, http.StatusAccepted, buffer.Status())
	assert.False(t, buffer.Written(), "WriteHeader follows Gin and does not write immediately")

	n, err := buffer.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	n, err = buffer.WriteString("def")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.True(t, buffer.Written())
	assert.Equal(t, 6, buffer.Size())
	buffer.WriteHeader(http.StatusTeapot)
	assert.Equal(t, http.StatusAccepted, buffer.Status(), "status cannot change after the body is written")

	buffer.Flush()
	assert.Empty(t, recorder.Body.String(), "Flush must not bypass durability")
	assert.Empty(t, recorder.Header().Get("X-Buffered"))

	require.NoError(t, buffer.Commit())
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "yes", recorder.Header().Get("X-Buffered"))
	assert.Equal(t, "kept", recorder.Header().Get("X-Before"))
	assert.Equal(t, "abcdef", recorder.Body.String())

	require.NoError(t, buffer.Commit())
	assert.Equal(t, "abcdef", recorder.Body.String(), "Commit must be idempotent")
}

func TestBufferedResponseWriterFlushAndDiscardDoNotLeakAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer, err := NewBufferedResponseWriter(context.Writer)
	require.NoError(t, err)

	buffer.Header().Set("X-Failed-Attempt", "secret")
	buffer.WriteHeader(http.StatusBadGateway)
	buffer.Flush()
	assert.True(t, buffer.Written())
	assert.Equal(t, 0, buffer.Size())
	assert.Equal(t, http.StatusBadGateway, buffer.Status())
	assert.Empty(t, recorder.Header().Get("X-Failed-Attempt"))
	assert.Empty(t, recorder.Body.String())

	buffer.Discard()
	_, err = buffer.WriteString("must not escape")
	require.ErrorIs(t, err, errBufferedResponseDiscarded)
	require.ErrorIs(t, buffer.Commit(), errBufferedResponseDiscarded)
	assert.Empty(t, recorder.Header().Get("X-Failed-Attempt"))
	assert.Empty(t, recorder.Body.String())
	assert.False(t, recorder.Flushed)
}

func TestBufferedResponseWriterSpillsToPrivateFileAndSnapshotsSafely(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	configureBufferedResponseTest(t, tempDir, 4, 32, 64, 2)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer, err := NewBufferedResponseWriter(context.Writer)
	require.NoError(t, err)
	t.Cleanup(buffer.Discard)

	buffer.Header().Set("Content-Type", "application/json")
	buffer.Header().Set("Set-Cookie", "secret=session")
	buffer.Header().Set("Connection", "X-Hop")
	buffer.Header().Set("X-Hop", "must-not-persist")
	_, err = buffer.WriteString("abc")
	require.NoError(t, err)
	assert.Nil(t, buffer.file)
	_, err = buffer.WriteString("def")
	require.NoError(t, err)
	require.NotNil(t, buffer.file)
	spoolPath := buffer.tempPath
	require.NoError(t, verifyBufferedResponsePathSecurity(spoolPath, false))

	status, headers, body, err := buffer.Snapshot(
		[]string{"content-type", "set-cookie", "connection", "x-hop"},
		16,
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "application/json", headers.Get("Content-Type"))
	assert.Empty(t, headers.Get("Set-Cookie"))
	assert.Empty(t, headers.Get("Connection"))
	assert.Empty(t, headers.Get("X-Hop"))
	assert.Equal(t, "abcdef", string(body))
	body[0] = 'X'
	_, _, secondBody, err := buffer.Snapshot([]string{"Content-Type"}, 16)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(secondBody), "snapshot body must be a private copy")

	require.NoError(t, buffer.Commit())
	assert.Equal(t, "abcdef", recorder.Body.String())
	require.ErrorIs(t, func() error {
		_, statErr := os.Stat(spoolPath)
		return statErr
	}(), os.ErrNotExist)
	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	bufferedResponseGate.Unlock()
}

func TestBufferedResponseWriterHardLimitFailsWithoutPublishingAndCleansSpool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureBufferedResponseTest(t, t.TempDir(), 4, 8, 8, 1)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer, err := NewBufferedResponseWriter(context.Writer)
	require.NoError(t, err)
	_, err = buffer.WriteString("12345")
	require.NoError(t, err)
	spoolPath := buffer.tempPath
	require.NotEmpty(t, spoolPath)

	_, err = buffer.WriteString("6789")
	require.ErrorIs(t, err, ErrBufferedResponseTooLarge)
	require.ErrorIs(t, buffer.Commit(), ErrBufferedResponseTooLarge)
	assert.Empty(t, recorder.Body.String())
	_, statErr := os.Stat(spoolPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	bufferedResponseGate.Unlock()
}

func TestBufferedResponseReservationGateAndStorageProbeFailFast(t *testing.T) {
	configureBufferedResponseTest(t, t.TempDir(), 4, 8, 16, 2)
	first, err := AcquireBufferedResponseReservation()
	require.NoError(t, err)
	second, err := AcquireBufferedResponseReservation()
	require.NoError(t, err)
	_, err = AcquireBufferedResponseReservation()
	require.ErrorIs(t, err, ErrBufferedResponseCapacity)
	first.Release()
	replacement, err := AcquireBufferedResponseReservation()
	require.NoError(t, err)
	first.Release()
	second.Release()
	replacement.Release()

	notDirectory := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(notDirectory, []byte("x"), 0o600))
	configureBufferedResponseTest(t, notDirectory, 4, 8, 16, 2)
	_, err = AcquireBufferedResponseReservation()
	require.ErrorIs(t, err, ErrBufferedResponseCapacity)
	require.ErrorIs(t, err, ErrBufferedResponseStorage)
	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	bufferedResponseGate.Unlock()
}

func TestContextBufferedWritersAdmitNormalResponsesBeyondWorstCaseLimit(t *testing.T) {
	configureBufferedResponseTest(t, t.TempDir(), 1, 8, 32, 64)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	writers := make([]*BufferedResponseWriter, 0, 5)
	for range 5 {
		writer, err := NewBufferedResponseWriterContext(ctx, newGinTestWriter())
		require.NoError(t, err)
		writers = append(writers, writer)
	}
	for _, writer := range writers {
		writer.Discard()
	}

	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	bufferedResponseGate.Unlock()
}

func TestContextBufferedWriterUpgradeWaitsForLargeResponseCapacity(t *testing.T) {
	configureBufferedResponseTest(t, t.TempDir(), 4, 8, 12, 3)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	first, err := NewBufferedResponseWriterContext(ctx, newGinTestWriter())
	require.NoError(t, err)
	second, err := NewBufferedResponseWriterContext(ctx, newGinTestWriter())
	require.NoError(t, err)
	t.Cleanup(second.Discard)

	_, err = first.WriteString("12345")
	require.NoError(t, err)
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := second.WriteString("12345")
		writeDone <- writeErr
	}()

	select {
	case err := <-writeDone:
		t.Fatalf("second large response bypassed aggregate reservation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	first.Discard()
	select {
	case err := <-writeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second response did not resume after capacity was released")
	}
}

func newGinTestWriter() gin.ResponseWriter {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	return context.Writer
}

func TestBufferedResponseReservationRejectsCapacityArithmeticOverflow(t *testing.T) {
	configureBufferedResponseTest(t, t.TempDir(), 1, math.MaxInt64, math.MaxInt64, 1)
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "1")

	_, err := AcquireBufferedResponseReservation()
	require.ErrorIs(t, err, ErrBufferedResponseCapacity)
	assert.Contains(t, err.Error(), "overflows int64")
	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	assert.Empty(t, bufferedResponseGate.spoolDir, "overflow must fail before creating spool state")
	bufferedResponseGate.Unlock()
}

func TestBufferedResponseConfigRejectsStaleDurationOverflow(t *testing.T) {
	if int64(int(math.MaxInt64)) != math.MaxInt64 {
		t.Skip("extreme duration configuration requires a 64-bit int")
	}
	t.Setenv("RELAY_RESPONSE_BUFFER_STALE_SECONDS", fmt.Sprintf("%d", math.MaxInt64))

	config := loadBufferedResponseConfig()
	assert.Equal(t, defaultBufferedResponseStaleSeconds, config.staleSeconds)
	assert.Positive(t, time.Duration(config.staleSeconds)*time.Second)
}

type failingBufferedDownstream struct {
	gin.ResponseWriter
	err error
}

func (w *failingBufferedDownstream) Write([]byte) (int, error) {
	return 0, w.err
}

func TestBufferedResponseWriterPanicDiscardAndCommitWriteErrorAlwaysCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	configureBufferedResponseTest(t, tempDir, 4, 16, 16, 1)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	buffer, err := NewBufferedResponseWriter(context.Writer)
	require.NoError(t, err)
	_, err = buffer.WriteString("spilled")
	require.NoError(t, err)
	panicPath := buffer.tempPath
	func() {
		defer func() {
			buffer.Discard()
			assert.Equal(t, "boom", recover())
		}()
		panic("boom")
	}()
	_, statErr := os.Stat(panicPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	writeFailure := errors.New("downstream write failed")
	failing := &failingBufferedDownstream{ResponseWriter: context.Writer, err: writeFailure}
	second, err := NewBufferedResponseWriter(failing)
	require.NoError(t, err)
	_, err = second.WriteString("spilled-again")
	require.NoError(t, err)
	writeErrorPath := second.tempPath
	require.ErrorIs(t, second.Commit(), writeFailure)
	_, statErr = os.Stat(writeErrorPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	bufferedResponseGate.Lock()
	assert.Zero(t, bufferedResponseGate.active)
	assert.Zero(t, bufferedResponseGate.reservedBytes)
	bufferedResponseGate.Unlock()
}

func TestBufferedResponseSpoolStaleCleanupIsPrefixAgeAndLockSafe(t *testing.T) {
	baseDir := t.TempDir()
	now := time.Now()
	oldTime := now.Add(-2 * time.Hour)
	oldDir := filepath.Join(baseDir, bufferedResponseInstancePrefix+"old")
	require.NoError(t, os.Mkdir(oldDir, 0o700))
	require.NoError(t, secureBufferedResponsePath(oldDir, true))
	oldLock, err := createBufferedResponseOwnershipLock(filepath.Join(oldDir, ".active.lock"))
	require.NoError(t, err)
	require.NoError(t, secureBufferedResponsePath(oldLock.Name(), false))
	require.NoError(t, oldLock.Close())
	require.NoError(t, os.Chtimes(oldDir, oldTime, oldTime))

	activeDir := filepath.Join(baseDir, bufferedResponseInstancePrefix+"active")
	require.NoError(t, os.Mkdir(activeDir, 0o700))
	require.NoError(t, secureBufferedResponsePath(activeDir, true))
	activeLock, err := createBufferedResponseOwnershipLock(filepath.Join(activeDir, ".active.lock"))
	require.NoError(t, err)
	require.NoError(t, secureBufferedResponsePath(activeLock.Name(), false))
	locked, err := tryLockBufferedResponseFile(activeLock)
	require.NoError(t, err)
	require.True(t, locked)
	require.NoError(t, os.Chtimes(activeDir, oldTime, oldTime))

	recentDir := filepath.Join(baseDir, bufferedResponseInstancePrefix+"recent")
	require.NoError(t, os.Mkdir(recentDir, 0o700))
	unrelated := filepath.Join(baseDir, "unrelated.data")
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0o600))
	symlinkTarget := filepath.Join(baseDir, "symlink-target")
	require.NoError(t, os.Mkdir(symlinkTarget, 0o700))
	symlinkPath := filepath.Join(baseDir, bufferedResponseInstancePrefix+"symlink")
	symlinkCreated := os.Symlink(symlinkTarget, symlinkPath) == nil

	configureBufferedResponseTest(t, baseDir, 4, 8, 16, 2)
	t.Setenv("RELAY_RESPONSE_BUFFER_STALE_SECONDS", "3600")
	reservation, err := AcquireBufferedResponseReservation()
	require.NoError(t, err)
	reservation.Release()
	_, err = os.Stat(oldDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(activeDir)
	require.NoError(t, err, "an old directory with a held ownership lock is active")
	_, err = os.Stat(recentDir)
	require.NoError(t, err)
	_, err = os.Stat(unrelated)
	require.NoError(t, err)
	if symlinkCreated {
		_, err = os.Lstat(symlinkPath)
		require.NoError(t, err, "cleanup must refuse matching symlinks")
		_, err = os.Stat(symlinkTarget)
		require.NoError(t, err)
	}

	require.NoError(t, activeLock.Close())
	require.NoError(t, cleanupStaleBufferedResponseSpoolDirs(baseDir, time.Hour, now))
	_, err = os.Stat(activeDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(baseDir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, strings.Contains(entry.Name(), ".deleting-"), "stale cleanup must not leave an unlocked quarantine directory")
	}
}
