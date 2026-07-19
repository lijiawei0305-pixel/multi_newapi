package logger

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogMaxBytes   int64 = 100 * 1024 * 1024
	defaultLogMaxAge           = 30 * 24 * time.Hour
	defaultLogMaxBackups       = 10
	activeLogFileName          = "oneapi-current.log"
)

type rotatingFileWriter struct {
	mu         sync.Mutex
	dir        string
	activePath string
	maxBytes   int64
	maxAge     time.Duration
	maxBackups int
	file       *os.File
	size       int64
}

func newRotatingFileWriter(dir string, maxBytes int64, maxAge time.Duration, maxBackups int) (*rotatingFileWriter, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("log maximum size must be positive")
	}
	if maxAge < 0 {
		return nil, fmt.Errorf("log maximum age cannot be negative")
	}
	if maxBackups < 0 {
		return nil, fmt.Errorf("log maximum backups cannot be negative")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}

	w := &rotatingFileWriter{
		dir:        dir,
		activePath: filepath.Join(dir, activeLogFileName),
		maxBytes:   maxBytes,
		maxAge:     maxAge,
		maxBackups: maxBackups,
	}
	if err := w.openActiveLocked(); err != nil {
		return nil, err
	}
	if w.size >= w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			_ = w.file.Close()
			return nil, err
		}
	} else if err := w.cleanupLocked(time.Now()); err != nil {
		_ = w.file.Close()
		return nil, err
	}
	return w, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, fs.ErrClosed
	}
	written := 0
	for len(p) > 0 {
		if w.size >= w.maxBytes {
			if err := w.rotateLocked(); err != nil {
				return written, err
			}
		}

		remaining := w.maxBytes - w.size
		chunkSize := int64(len(p))
		if chunkSize > remaining {
			chunkSize = remaining
		}
		n, err := w.file.Write(p[:int(chunkSize)])
		w.size += int64(n)
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := errors.Join(w.file.Sync(), w.file.Close())
	w.file = nil
	return err
}

func (w *rotatingFileWriter) openActiveLocked() error {
	file, err := os.OpenFile(w.activePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open active log file: %w", err)
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure active log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat active log file: %w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingFileWriter) rotateLocked() error {
	if w.file == nil {
		return fs.ErrClosed
	}
	if err := errors.Join(w.file.Sync(), w.file.Close()); err != nil {
		w.file = nil
		return fmt.Errorf("close active log file: %w", err)
	}
	w.file = nil

	archivePath, err := w.availableArchivePathLocked(time.Now())
	if err != nil {
		return err
	}
	if err := os.Rename(w.activePath, archivePath); err != nil {
		return fmt.Errorf("archive active log file: %w", err)
	}
	if err := w.openActiveLocked(); err != nil {
		return err
	}
	if err := w.cleanupLocked(time.Now()); err != nil {
		return err
	}
	return nil
}

func (w *rotatingFileWriter) availableArchivePathLocked(now time.Time) (string, error) {
	base := "oneapi-" + now.UTC().Format("20060102T150405.000000000")
	for attempt := 0; attempt < 1000; attempt++ {
		name := base + ".log"
		if attempt > 0 {
			name = fmt.Sprintf("%s-%03d.log", base, attempt)
		}
		path := filepath.Join(w.dir, name)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("check archived log path: %w", err)
		}
	}
	return "", fmt.Errorf("cannot allocate archived log path")
}

func (w *rotatingFileWriter) cleanupLocked(now time.Time) error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("read log directory: %w", err)
	}

	type archivedLog struct {
		path    string
		modTime time.Time
	}
	archives := make([]archivedLog, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == activeLogFileName || !strings.HasPrefix(name, "oneapi-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat archived log %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(w.dir, name)
		if err := os.Chmod(path, 0600); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("secure archived log %q: %w", name, err)
		}
		if w.maxAge > 0 && now.Sub(info.ModTime()) > w.maxAge {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove expired log %q: %w", name, err)
			}
			continue
		}
		archives = append(archives, archivedLog{path: path, modTime: info.ModTime()})
	}

	sort.Slice(archives, func(i, j int) bool {
		if archives[i].modTime.Equal(archives[j].modTime) {
			return archives[i].path > archives[j].path
		}
		return archives[i].modTime.After(archives[j].modTime)
	})
	if len(archives) > w.maxBackups {
		for _, archive := range archives[w.maxBackups:] {
			if err := os.Remove(archive.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove excess log %q: %w", filepath.Base(archive.path), err)
			}
		}
	}
	return nil
}
