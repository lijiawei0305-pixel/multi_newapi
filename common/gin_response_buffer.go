package common

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultBufferedResponseMemoryBytes   = int64(1 << 20)
	defaultBufferedResponseMaxBytes      = int64(256 << 20)
	defaultBufferedResponseTotalBytes    = int64(1 << 30)
	defaultBufferedResponseDiskSafety    = int64(16 << 20)
	defaultBufferedResponseMaxConcurrent = 64
	defaultBufferedResponseStaleSeconds  = 24 * 60 * 60
	bufferedResponseInstancePrefix       = "new-api-relay-response-instance-"
)

var (
	errBufferedResponseCommitted = errors.New("buffered response is already committed")
	errBufferedResponseDiscarded = errors.New("buffered response is discarded")

	ErrBufferedResponseCapacity = errors.New("buffered response capacity is exhausted")
	ErrBufferedResponseTooLarge = errors.New("buffered response exceeds the per-request limit")
	ErrBufferedResponseStorage  = errors.New("buffered response spool storage is unavailable")
)

type bufferedResponseConfig struct {
	memoryBytes   int64
	maxBytes      int64
	totalBytes    int64
	diskSafety    int64
	maxConcurrent int
	staleSeconds  int
	tempDir       string
}

var bufferedResponseGate struct {
	sync.Mutex
	active        int
	reservedBytes int64
	spoolBaseDir  string
	spoolDir      string
	spoolLock     *os.File
	capacityFreed chan struct{}
}

func loadBufferedResponseConfig() bufferedResponseConfig {
	config := bufferedResponseConfig{
		memoryBytes:   int64(GetEnvOrDefault("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", int(defaultBufferedResponseMemoryBytes))),
		maxBytes:      int64(GetEnvOrDefault("RELAY_RESPONSE_BUFFER_MAX_BYTES", int(defaultBufferedResponseMaxBytes))),
		totalBytes:    int64(GetEnvOrDefault("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", int(defaultBufferedResponseTotalBytes))),
		diskSafety:    int64(GetEnvOrDefault("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", int(defaultBufferedResponseDiskSafety))),
		maxConcurrent: GetEnvOrDefault("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", defaultBufferedResponseMaxConcurrent),
		staleSeconds:  GetEnvOrDefault("RELAY_RESPONSE_BUFFER_STALE_SECONDS", defaultBufferedResponseStaleSeconds),
		tempDir:       GetEnvOrDefaultString("RELAY_RESPONSE_BUFFER_TEMP_DIR", ""),
	}
	if config.memoryBytes <= 0 {
		config.memoryBytes = defaultBufferedResponseMemoryBytes
	}
	if config.maxBytes <= 0 {
		config.maxBytes = defaultBufferedResponseMaxBytes
	}
	if config.totalBytes <= 0 {
		config.totalBytes = defaultBufferedResponseTotalBytes
	}
	if config.diskSafety < 0 {
		config.diskSafety = defaultBufferedResponseDiskSafety
	}
	if config.maxConcurrent <= 0 {
		config.maxConcurrent = defaultBufferedResponseMaxConcurrent
	}
	maxStaleSeconds := int64(math.MaxInt64) / int64(time.Second)
	if config.staleSeconds <= 0 || int64(config.staleSeconds) > maxStaleSeconds {
		config.staleSeconds = defaultBufferedResponseStaleSeconds
	}
	if config.memoryBytes > config.maxBytes {
		config.memoryBytes = config.maxBytes
	}
	return config
}

// BufferedResponseBodyLimit returns the hard decoded body limit used by the
// response spool. Provider-specific decoders can use it to bound upstream
// success payloads before writing into the spool.
func BufferedResponseBodyLimit() int64 {
	return loadBufferedResponseConfig().maxBytes
}

// BufferedResponseReservation owns a request's share of the aggregate response
// spool. Task/idempotency callers reserve the full worst-case body before they
// contact a provider. Ordinary synchronous relays start with their in-memory
// allowance and atomically upgrade to the full allowance before spilling.
type BufferedResponseReservation struct {
	config        bufferedResponseConfig
	ctx           context.Context
	expandable    bool
	mu            sync.Mutex
	reservedBytes int64
	released      bool
	once          sync.Once
}

func AcquireBufferedResponseReservation() (*BufferedResponseReservation, error) {
	return acquireBufferedResponseReservation(nil, false)
}

func acquireBufferedResponseReservation(ctx context.Context, expandable bool) (*BufferedResponseReservation, error) {
	config := loadBufferedResponseConfig()
	reservationBytes := config.maxBytes
	upgradeHeadroom := int64(0)
	if expandable {
		reservationBytes = config.memoryBytes
		upgradeHeadroom = config.maxBytes - reservationBytes
	}
	bufferedResponseGate.Lock()
	defer bufferedResponseGate.Unlock()
	if bufferedResponseGate.capacityFreed == nil {
		bufferedResponseGate.capacityFreed = make(chan struct{})
	}
	if bufferedResponseGate.active >= config.maxConcurrent ||
		reservationBytes > config.totalBytes ||
		upgradeHeadroom > config.totalBytes-reservationBytes ||
		bufferedResponseGate.reservedBytes > config.totalBytes-reservationBytes-upgradeHeadroom {
		return nil, fmt.Errorf(
			"%w: active=%d/%d reserved=%d max_total=%d",
			ErrBufferedResponseCapacity,
			bufferedResponseGate.active,
			config.maxConcurrent,
			bufferedResponseGate.reservedBytes,
			config.totalBytes,
		)
	}
	newReservedBytes := bufferedResponseGate.reservedBytes + reservationBytes
	preflightReservedBytes := newReservedBytes + upgradeHeadroom
	if config.diskSafety > math.MaxInt64-preflightReservedBytes {
		return nil, fmt.Errorf(
			"%w: required spool capacity overflows int64: reserved=%d request=%d safety=%d",
			ErrBufferedResponseCapacity,
			bufferedResponseGate.reservedBytes,
			reservationBytes+upgradeHeadroom,
			config.diskSafety,
		)
	}
	spoolDir, err := ensureBufferedResponseSpoolDirLocked(config.tempDir, time.Duration(config.staleSeconds)*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBufferedResponseCapacity, err)
	}
	config.tempDir = spoolDir
	requiredDiskBytes := preflightReservedBytes + config.diskSafety
	if err := preflightBufferedResponseSpool(config.tempDir, requiredDiskBytes); err != nil {
		if bufferedResponseGate.active == 0 {
			cleanupBufferedResponseSpoolDirLocked()
		}
		return nil, fmt.Errorf("%w: %w", ErrBufferedResponseCapacity, err)
	}
	bufferedResponseGate.active++
	bufferedResponseGate.reservedBytes = newReservedBytes
	if ctx == nil {
		ctx = context.Background()
	}
	return &BufferedResponseReservation{
		config:        config,
		ctx:           ctx,
		expandable:    expandable,
		reservedBytes: reservationBytes,
	}, nil
}

func ensureBufferedResponseSpoolDirLocked(baseDir string, staleAge time.Duration) (string, error) {
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	baseDir, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("%w: resolve spool base directory: %v", ErrBufferedResponseStorage, err)
	}
	baseDir, err = filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("%w: resolve spool base directory links: %v", ErrBufferedResponseStorage, err)
	}
	if bufferedResponseGate.spoolDir != "" {
		if bufferedResponseGate.spoolBaseDir != baseDir {
			return "", fmt.Errorf("%w: active spool base directory changed", ErrBufferedResponseStorage)
		}
		return bufferedResponseGate.spoolDir, nil
	}
	baseInfo, err := os.Stat(baseDir)
	if err != nil {
		return "", fmt.Errorf("%w: stat base directory %s: %v", ErrBufferedResponseStorage, baseDir, err)
	}
	if !baseInfo.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrBufferedResponseStorage, baseDir)
	}
	if err := verifyBufferedResponseBaseDirSecurity(baseDir); err != nil {
		return "", fmt.Errorf("%w: unsafe spool base directory: %v", ErrBufferedResponseStorage, err)
	}
	if err := cleanupStaleBufferedResponseSpoolDirs(baseDir, staleAge, time.Now()); err != nil {
		return "", err
	}
	dir, err := createPrivateBufferedResponseTempDir(baseDir, bufferedResponseInstancePrefix)
	if err != nil {
		return "", fmt.Errorf("%w: create private spool directory: %v", ErrBufferedResponseStorage, err)
	}
	cleanupDir := func() {
		_ = os.RemoveAll(dir)
	}
	lockFile, err := createBufferedResponseOwnershipLock(filepath.Join(dir, ".active.lock"))
	if err != nil {
		cleanupDir()
		return "", fmt.Errorf("%w: create spool ownership lock: %v", ErrBufferedResponseStorage, err)
	}
	if err := secureBufferedResponsePath(lockFile.Name(), false); err != nil {
		_ = lockFile.Close()
		cleanupDir()
		return "", fmt.Errorf("%w: secure spool ownership lock: %v", ErrBufferedResponseStorage, err)
	}
	locked, err := tryLockBufferedResponseFile(lockFile)
	if err != nil || !locked {
		_ = lockFile.Close()
		cleanupDir()
		if err == nil {
			err = errors.New("ownership lock is busy")
		}
		return "", fmt.Errorf("%w: lock private spool directory: %v", ErrBufferedResponseStorage, err)
	}
	bufferedResponseGate.spoolBaseDir = baseDir
	bufferedResponseGate.spoolDir = dir
	bufferedResponseGate.spoolLock = lockFile
	return dir, nil
}

func cleanupStaleBufferedResponseSpoolDirs(baseDir string, staleAge time.Duration, now time.Time) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("%w: list spool base directory: %v", ErrBufferedResponseStorage, err)
	}
	cutoff := now.Add(-staleAge)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), bufferedResponseInstancePrefix) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(baseDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			SysError(fmt.Sprintf("skip inaccessible stale response spool directory: error_type=%T", err))
			continue
		}
		if !info.IsDir() || info.ModTime().After(cutoff) {
			continue
		}
		if err := verifyBufferedResponsePathSecurity(path, true); err != nil {
			SysError(fmt.Sprintf("skip unsafe stale response spool directory: error_type=%T", err))
			continue
		}
		lockPath := filepath.Join(path, ".active.lock")
		lockInfo, err := os.Lstat(lockPath)
		if err == nil && lockInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			SysError(fmt.Sprintf("skip inaccessible stale response spool lock: error_type=%T", err))
			continue
		}
		var lockFile *os.File
		if err == nil {
			if securityErr := verifyBufferedResponsePathSecurity(lockPath, false); securityErr != nil {
				SysError(fmt.Sprintf("skip unsafe stale response spool lock: error_type=%T", securityErr))
				continue
			}
			lockFile, err = openBufferedResponseOwnershipLock(lockPath)
		} else {
			lockFile, err = createBufferedResponseOwnershipLock(lockPath)
			if err == nil {
				if securityErr := secureBufferedResponsePath(lockPath, false); securityErr != nil {
					_ = lockFile.Close()
					_ = os.Remove(lockPath)
					SysError(fmt.Sprintf("skip stale response spool lock security failure: error_type=%T", securityErr))
					continue
				}
			}
		}
		if err != nil {
			continue
		}
		locked, lockErr := tryLockBufferedResponseFile(lockFile)
		if lockErr != nil || !locked {
			if closeErr := lockFile.Close(); closeErr != nil {
				SysError(fmt.Sprintf("close stale response spool lock failure: error_type=%T", closeErr))
			}
			continue
		}
		if removeErr := removeLockedBufferedResponseSpoolDir(path, lockFile); removeErr != nil {
			SysError(fmt.Sprintf("skip stale response spool cleanup failure: error_type=%T", removeErr))
		}
	}
	return nil
}

func cleanupBufferedResponseSpoolDirLocked() {
	if bufferedResponseGate.spoolLock != nil && bufferedResponseGate.spoolDir != "" {
		if err := removeLockedBufferedResponseSpoolDir(bufferedResponseGate.spoolDir, bufferedResponseGate.spoolLock); err != nil {
			SysError(fmt.Sprintf("cleanup response spool directory failure: error_type=%T", err))
		}
	} else if bufferedResponseGate.spoolLock != nil {
		if err := bufferedResponseGate.spoolLock.Close(); err != nil {
			SysError(fmt.Sprintf("close response spool ownership lock failure: error_type=%T", err))
		}
	} else if bufferedResponseGate.spoolDir != "" {
		SysError("response spool directory has no ownership lock; refusing unsafe cleanup")
	}
	bufferedResponseGate.spoolBaseDir = ""
	bufferedResponseGate.spoolDir = ""
	bufferedResponseGate.spoolLock = nil
}

func preflightBufferedResponseSpool(tempDir string, requiredBytes int64) error {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	availableBytes, err := bufferedResponseAvailableDiskBytes(tempDir)
	if err != nil {
		return fmt.Errorf("%w: inspect %s: %v", ErrBufferedResponseStorage, tempDir, err)
	}
	if availableBytes < requiredBytes {
		return fmt.Errorf("%w: directory=%s available=%d required=%d", ErrBufferedResponseStorage, tempDir, availableBytes, requiredBytes)
	}
	probe, err := os.CreateTemp(tempDir, "new-api-relay-response-probe-*")
	if err != nil {
		return fmt.Errorf("%w: create probe in %s: %v", ErrBufferedResponseStorage, tempDir, err)
	}
	path := probe.Name()
	cleanup := func() error {
		closeErr := probe.Close()
		removeErr := os.Remove(path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return errors.Join(closeErr, removeErr)
	}
	if err := secureBufferedResponsePath(path, false); err != nil {
		return errors.Join(fmt.Errorf("%w: secure probe: %v", ErrBufferedResponseStorage, err), cleanup())
	}
	if err := cleanup(); err != nil {
		return fmt.Errorf("%w: cleanup probe: %v", ErrBufferedResponseStorage, err)
	}
	return nil
}

func (r *BufferedResponseReservation) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		bufferedResponseGate.Lock()
		r.mu.Lock()
		r.released = true
		bufferedResponseGate.active--
		bufferedResponseGate.reservedBytes -= r.reservedBytes
		r.mu.Unlock()
		if bufferedResponseGate.active == 0 {
			cleanupBufferedResponseSpoolDirLocked()
		}
		if bufferedResponseGate.capacityFreed != nil {
			close(bufferedResponseGate.capacityFreed)
			bufferedResponseGate.capacityFreed = make(chan struct{})
		}
		bufferedResponseGate.Unlock()
	})
}

// reserveForSize upgrades an adaptive reservation to its full per-request
// allowance before the response leaves memory. Upgrades wait for a completed
// request to release aggregate capacity, so several large responses cannot
// overcommit the spool or strand every writer with a partial reservation.
func (r *BufferedResponseReservation) reserveForSize(requiredBytes int64) error {
	if r == nil {
		return errors.New("buffered response reservation is nil")
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return fmt.Errorf("%w: response reservation upgrade canceled: %w", ErrBufferedResponseCapacity, err)
		}
		bufferedResponseGate.Lock()
		if bufferedResponseGate.capacityFreed == nil {
			bufferedResponseGate.capacityFreed = make(chan struct{})
		}
		r.mu.Lock()
		if r.released {
			r.mu.Unlock()
			bufferedResponseGate.Unlock()
			return errBufferedResponseDiscarded
		}
		if requiredBytes <= r.reservedBytes {
			r.mu.Unlock()
			bufferedResponseGate.Unlock()
			return nil
		}
		if !r.expandable {
			r.mu.Unlock()
			bufferedResponseGate.Unlock()
			return fmt.Errorf("%w: response exceeds its reserved capacity", ErrBufferedResponseCapacity)
		}
		additionalBytes := r.config.maxBytes - r.reservedBytes
		if additionalBytes <= r.config.totalBytes &&
			bufferedResponseGate.reservedBytes <= r.config.totalBytes-additionalBytes {
			newReservedBytes := bufferedResponseGate.reservedBytes + additionalBytes
			if r.config.diskSafety > math.MaxInt64-newReservedBytes {
				r.mu.Unlock()
				bufferedResponseGate.Unlock()
				return fmt.Errorf("%w: required spool capacity overflows int64", ErrBufferedResponseCapacity)
			}
			if err := preflightBufferedResponseSpool(r.config.tempDir, newReservedBytes+r.config.diskSafety); err != nil {
				r.mu.Unlock()
				bufferedResponseGate.Unlock()
				return fmt.Errorf("%w: %w", ErrBufferedResponseCapacity, err)
			}
			bufferedResponseGate.reservedBytes = newReservedBytes
			r.reservedBytes = r.config.maxBytes
			r.mu.Unlock()
			bufferedResponseGate.Unlock()
			return nil
		}
		capacityFreed := bufferedResponseGate.capacityFreed
		r.mu.Unlock()
		bufferedResponseGate.Unlock()
		select {
		case <-r.ctx.Done():
			return fmt.Errorf("%w: response reservation upgrade canceled: %w", ErrBufferedResponseCapacity, r.ctx.Err())
		case <-capacityFreed:
		}
	}
}

// BufferedResponseWriter keeps a Gin response private until Commit is called.
// Bodies remain in memory only up to the configured threshold, then spill to a
// request-private 0600 temporary file. The writer owns its reservation.
type BufferedResponseWriter struct {
	downstream  gin.ResponseWriter
	header      http.Header
	memory      bytes.Buffer
	file        *os.File
	tempPath    string
	reservation *BufferedResponseReservation
	status      int
	size        int
	writeErr    error
	commitErr   error
	committed   bool
	discarded   bool
}

var _ gin.ResponseWriter = (*BufferedResponseWriter)(nil)

func NewBufferedResponseWriter(downstream gin.ResponseWriter) (*BufferedResponseWriter, error) {
	reservation, err := AcquireBufferedResponseReservation()
	if err != nil {
		return nil, err
	}
	writer, err := NewBufferedResponseWriterWithReservation(downstream, reservation)
	if err != nil {
		reservation.Release()
	}
	return writer, err
}

// NewBufferedResponseWriterContext admits ordinary synchronous relays with a
// small initial reservation. This avoids a four-request global bottleneck for
// normal text responses while retaining the full hard limit for large bodies.
func NewBufferedResponseWriterContext(ctx context.Context, downstream gin.ResponseWriter) (*BufferedResponseWriter, error) {
	reservation, err := acquireBufferedResponseReservation(ctx, true)
	if err != nil {
		return nil, err
	}
	writer, err := NewBufferedResponseWriterWithReservation(downstream, reservation)
	if err != nil {
		reservation.Release()
	}
	return writer, err
}

func NewBufferedResponseWriterWithReservation(downstream gin.ResponseWriter, reservation *BufferedResponseReservation) (*BufferedResponseWriter, error) {
	if downstream == nil {
		return nil, errors.New("buffered response downstream writer is nil")
	}
	if reservation == nil {
		return nil, errors.New("buffered response reservation is nil")
	}
	return &BufferedResponseWriter{
		downstream:  downstream,
		header:      downstream.Header().Clone(),
		reservation: reservation,
		status:      http.StatusOK,
		size:        -1,
	}, nil
}

func (w *BufferedResponseWriter) Underlying() gin.ResponseWriter {
	return w.downstream
}

func (w *BufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *BufferedResponseWriter) WriteHeader(code int) {
	if code <= 0 || w.Written() || w.committed || w.discarded {
		return
	}
	w.status = code
}

func (w *BufferedResponseWriter) WriteHeaderNow() {
	if !w.Written() && !w.committed && !w.discarded {
		w.size = 0
	}
}

func (w *BufferedResponseWriter) Write(data []byte) (int, error) {
	destination, err := w.prepareWrite(len(data))
	if err != nil {
		return 0, err
	}
	n, writeErr := destination.Write(data)
	return w.finishWrite(n, len(data), writeErr)
}

func (w *BufferedResponseWriter) WriteString(data string) (int, error) {
	destination, err := w.prepareWrite(len(data))
	if err != nil {
		return 0, err
	}
	n, writeErr := io.WriteString(destination, data)
	return w.finishWrite(n, len(data), writeErr)
}

func (w *BufferedResponseWriter) prepareWrite(length int) (io.Writer, error) {
	if err := w.writeError(); err != nil {
		return nil, err
	}
	w.WriteHeaderNow()
	if int64(w.size) > w.reservation.config.maxBytes-int64(length) {
		attemptedBytes := int64(w.size)
		if int64(length) > math.MaxInt64-attemptedBytes {
			attemptedBytes = math.MaxInt64
		} else {
			attemptedBytes += int64(length)
		}
		w.writeErr = fmt.Errorf("%w: limit=%d attempted_at_least=%d", ErrBufferedResponseTooLarge, w.reservation.config.maxBytes, attemptedBytes)
		_ = w.cleanupStorage()
		return nil, w.writeErr
	}
	requiredBytes := int64(w.size) + int64(length)
	if err := w.reservation.reserveForSize(requiredBytes); err != nil {
		w.writeErr = err
		_ = w.cleanupStorage()
		return nil, w.writeErr
	}
	if w.file == nil && int64(w.memory.Len()+length) > w.reservation.config.memoryBytes {
		file, err := os.CreateTemp(w.reservation.config.tempDir, "new-api-relay-response-*")
		if err != nil {
			w.writeErr = fmt.Errorf("create buffered response spool: %w", err)
			return nil, w.writeErr
		}
		if err := secureBufferedResponsePath(file.Name(), false); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			w.writeErr = fmt.Errorf("secure buffered response spool: %w", err)
			return nil, w.writeErr
		}
		if w.memory.Len() > 0 {
			n, err := file.Write(w.memory.Bytes())
			if err != nil || n != w.memory.Len() {
				if err == nil {
					err = io.ErrShortWrite
				}
				_ = file.Close()
				_ = os.Remove(file.Name())
				w.writeErr = fmt.Errorf("spill buffered response: %w", err)
				return nil, w.writeErr
			}
		}
		w.memory.Reset()
		w.file = file
		w.tempPath = file.Name()
	}
	if w.file != nil {
		return w.file, nil
	}
	return &w.memory, nil
}

func (w *BufferedResponseWriter) finishWrite(written int, expected int, err error) (int, error) {
	w.size += written
	if err == nil && written != expected {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.writeErr = fmt.Errorf("write buffered response: %w", err)
		_ = w.cleanupStorage()
	}
	return written, w.writeErr
}

func (w *BufferedResponseWriter) Status() int {
	return w.status
}

func (w *BufferedResponseWriter) Size() int {
	return w.size
}

func (w *BufferedResponseWriter) Written() bool {
	return w.size != -1
}

func (w *BufferedResponseWriter) Err() error {
	return w.writeErr
}

// Fail records a post-provider local delivery failure while keeping the
// reservation alive until the caller completes durable billing and then calls
// Commit or Discard.
func (w *BufferedResponseWriter) Fail(err error) error {
	if err == nil {
		return nil
	}
	if stateErr := w.writeError(); stateErr != nil {
		return stateErr
	}
	w.WriteHeaderNow()
	w.writeErr = fmt.Errorf("buffered response delivery failed: %w", err)
	_ = w.cleanupStorage()
	return w.writeErr
}

// Snapshot returns a private copy for durable idempotency storage without
// publishing, cleaning up, or releasing the reservation. Only explicitly
// allowed response headers are included.
func (w *BufferedResponseWriter) Snapshot(allowedHeaders []string, maxBytes int64) (status int, headers http.Header, body []byte, err error) {
	if w.discarded {
		return 0, nil, nil, errBufferedResponseDiscarded
	}
	if w.committed {
		return 0, nil, nil, errBufferedResponseCommitted
	}
	if w.writeErr != nil {
		return 0, nil, nil, w.writeErr
	}
	if maxBytes <= 0 || maxBytes > w.reservation.config.maxBytes {
		return 0, nil, nil, fmt.Errorf("buffered response snapshot limit must be between 1 and %d bytes", w.reservation.config.maxBytes)
	}
	if int64(max(w.size, 0)) > maxBytes {
		return 0, nil, nil, fmt.Errorf("%w: snapshot_limit=%d size=%d", ErrBufferedResponseTooLarge, maxBytes, max(w.size, 0))
	}

	forbiddenHeaders := map[string]struct{}{
		"Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {}, "Proxy-Authorization": {},
		"Set-Cookie": {}, "Set-Cookie2": {}, "Te": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	}
	for _, connectionValue := range w.header.Values("Connection") {
		for _, token := range bytes.Split([]byte(connectionValue), []byte{','}) {
			forbiddenHeaders[http.CanonicalHeaderKey(string(bytes.TrimSpace(token)))] = struct{}{}
		}
	}
	headers = make(http.Header, len(allowedHeaders))
	for _, key := range allowedHeaders {
		key = http.CanonicalHeaderKey(key)
		if _, forbidden := forbiddenHeaders[key]; forbidden || key == "" {
			continue
		}
		if values := w.header.Values(key); len(values) > 0 {
			headers[key] = append([]string(nil), values...)
		}
	}
	bodySize := max(w.size, 0)
	body = make([]byte, bodySize)
	if bodySize == 0 {
		return w.status, headers, body, nil
	}
	if w.file != nil {
		read, readErr := io.ReadFull(io.NewSectionReader(w.file, 0, int64(bodySize)), body)
		if readErr != nil {
			return 0, nil, nil, readErr
		}
		if read != bodySize {
			return 0, nil, nil, io.ErrUnexpectedEOF
		}
	} else {
		copy(body, w.memory.Bytes())
	}
	return w.status, headers, body, nil
}

// InspectBody provides a private bounded ReadSeeker view without copying the
// body or exposing a spool path. The callback must not retain the reader.
func (w *BufferedResponseWriter) InspectBody(inspect func(io.ReadSeeker) error) error {
	if inspect == nil {
		return errors.New("buffered response body inspector is nil")
	}
	if w.discarded {
		return errBufferedResponseDiscarded
	}
	if w.committed {
		return errBufferedResponseCommitted
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	bodySize := max(w.size, 0)
	if w.file != nil {
		return inspect(io.NewSectionReader(w.file, 0, int64(bodySize)))
	}
	return inspect(bytes.NewReader(w.memory.Bytes()))
}

// Flush records the same state transition as gin.ResponseWriter.Flush without
// sending headers or bytes to the network before Commit.
func (w *BufferedResponseWriter) Flush() {
	w.WriteHeaderNow()
}

func (w *BufferedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("cannot hijack a buffered response")
}

func (w *BufferedResponseWriter) CloseNotify() <-chan bool {
	return w.downstream.CloseNotify()
}

func (w *BufferedResponseWriter) Pusher() http.Pusher {
	return nil
}

// Commit publishes the buffered headers, status, and body exactly once, then
// removes any spool file and releases the process-level reservation.
func (w *BufferedResponseWriter) Commit() (err error) {
	if w.discarded {
		return errBufferedResponseDiscarded
	}
	if w.committed {
		return w.commitErr
	}
	w.committed = true
	defer func() {
		err = errors.Join(err, w.cleanupStorage())
		w.reservation.Release()
		w.commitErr = err
	}()
	if w.writeErr != nil {
		return w.writeErr
	}

	destination := w.downstream.Header()
	for key := range destination {
		delete(destination, key)
	}
	for key, values := range w.header {
		destination[key] = append([]string(nil), values...)
	}

	w.downstream.WriteHeader(w.status)
	if !w.Written() || w.size == 0 {
		w.downstream.WriteHeaderNow()
		return nil
	}
	if w.file != nil {
		if _, err := w.file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		written, err := io.Copy(w.downstream, w.file)
		if err != nil {
			return err
		}
		if written != int64(w.size) {
			return io.ErrShortWrite
		}
		return nil
	}
	written, err := w.downstream.Write(w.memory.Bytes())
	if err != nil {
		return err
	}
	if written != w.size {
		return io.ErrShortWrite
	}
	return nil
}

// Discard permanently drops a buffered attempt without touching the downstream
// writer, then releases all file and process capacity.
func (w *BufferedResponseWriter) Discard() {
	if w.committed || w.discarded {
		return
	}
	w.discarded = true
	if err := w.cleanupStorage(); err != nil {
		SysError(fmt.Sprintf("cleanup discarded buffered response failure: error_type=%T", err))
	}
	w.reservation.Release()
	clear(w.header)
	w.status = http.StatusOK
	w.size = -1
}

func (w *BufferedResponseWriter) cleanupStorage() error {
	w.memory.Reset()
	if w.file == nil {
		return nil
	}
	path := w.tempPath
	closeErr := w.file.Close()
	w.file = nil
	w.tempPath = ""
	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func (w *BufferedResponseWriter) writeError() error {
	if w.discarded {
		return errBufferedResponseDiscarded
	}
	if w.committed {
		return errBufferedResponseCommitted
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	return nil
}
