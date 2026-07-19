//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package common

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func createPrivateBufferedResponseTempDir(baseDir string, prefix string) (string, error) {
	dir, err := os.MkdirTemp(baseDir, prefix)
	if err != nil {
		return "", err
	}
	if err := secureBufferedResponsePath(dir, true); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func createBufferedResponseOwnershipLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
}

func openBufferedResponseOwnershipLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR, 0)
}

func removeLockedBufferedResponseSpoolDir(path string, lockFile *os.File) error {
	if lockFile == nil {
		return errors.New("stale response spool ownership lock is nil")
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		_ = lockFile.Close()
		return fmt.Errorf("generate stale spool quarantine name: %w", err)
	}
	quarantine := filepath.Join(filepath.Dir(path), filepath.Base(path)+".deleting-"+hex.EncodeToString(random))
	if err := os.Rename(path, quarantine); err != nil {
		return errors.Join(fmt.Errorf("quarantine stale response spool: %w", err), lockFile.Close())
	}
	closeErr := lockFile.Close()
	removeErr := os.RemoveAll(quarantine)
	return errors.Join(closeErr, removeErr)
}

func verifyBufferedResponseBaseDirSecurity(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("response spool base path is not a real directory")
	}
	owner, err := bufferedResponsePathOwnerUID(info)
	if err != nil {
		return err
	}
	if owner == uint32(os.Geteuid()) && info.Mode().Perm()&0o022 == 0 {
		return nil
	}
	if info.Mode()&os.ModeSticky != 0 && (owner == 0 || owner == uint32(os.Geteuid())) {
		return nil
	}
	return fmt.Errorf("response spool base directory is neither private to the current user nor a trusted sticky directory")
}

func secureBufferedResponsePath(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return verifyBufferedResponsePathSecurity(path, directory)
}

func verifyBufferedResponsePathSecurity(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private spool path is a symbolic link")
	}
	if directory && !info.IsDir() {
		return errors.New("private spool directory path is not a directory")
	}
	if !directory && !info.Mode().IsRegular() {
		return errors.New("private spool file path is not a regular file")
	}
	owner, err := bufferedResponsePathOwnerUID(info)
	if err != nil {
		return err
	}
	if owner != uint32(os.Geteuid()) {
		return fmt.Errorf("private spool path owner is %d, want %d", owner, os.Geteuid())
	}
	want := os.FileMode(0o600)
	if directory {
		want = 0o700
	}
	if got := info.Mode().Perm(); got != want {
		return fmt.Errorf("private spool permissions are %04o, want %04o", got, want)
	}
	return nil
}

func bufferedResponsePathOwnerUID(info os.FileInfo) (uint32, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, errors.New("private spool path owner is unavailable")
	}
	return stat.Uid, nil
}

func tryLockBufferedResponseFile(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
		return false, nil
	}
	return false, err
}

func bufferedResponseAvailableDiskBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	availableBlocks := uint64(stat.Bavail)
	blockSize := uint64(stat.Bsize)
	if blockSize != 0 && availableBlocks > math.MaxInt64/blockSize {
		return 0, fmt.Errorf("available spool capacity overflows int64")
	}
	return int64(availableBlocks * blockSize), nil
}
