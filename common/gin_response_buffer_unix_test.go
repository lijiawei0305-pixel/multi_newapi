//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package common

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBufferedResponseUnixSecurityRejectsUnsafeBaseLinksAndTypes(t *testing.T) {
	defaultTemp, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	require.NoError(t, verifyBufferedResponseBaseDirSecurity(defaultTemp), "the resolved default per-user or sticky system temp directory must remain supported")

	unsafeBase := filepath.Join(t.TempDir(), "unsafe-base")
	require.NoError(t, os.Mkdir(unsafeBase, 0o777))
	require.NoError(t, os.Chmod(unsafeBase, 0o777))
	require.Error(t, verifyBufferedResponseBaseDirSecurity(unsafeBase))

	privateDir := filepath.Join(t.TempDir(), "private-dir")
	require.NoError(t, os.Mkdir(privateDir, 0o700))
	require.NoError(t, verifyBufferedResponsePathSecurity(privateDir, true))
	require.Error(t, verifyBufferedResponsePathSecurity(privateDir, false))

	privateFile := filepath.Join(privateDir, "private-file")
	require.NoError(t, os.WriteFile(privateFile, nil, 0o600))
	require.NoError(t, verifyBufferedResponsePathSecurity(privateFile, false))
	require.Error(t, verifyBufferedResponsePathSecurity(privateFile, true))

	link := filepath.Join(t.TempDir(), "private-link")
	require.NoError(t, os.Symlink(privateDir, link))
	require.Error(t, verifyBufferedResponsePathSecurity(link, true))

	cleanupBase := t.TempDir()
	unsafeOldDir := filepath.Join(cleanupBase, bufferedResponseInstancePrefix+"unsafe")
	require.NoError(t, os.Mkdir(unsafeOldDir, 0o777))
	require.NoError(t, os.Chmod(unsafeOldDir, 0o777))
	require.NoError(t, os.Chtimes(unsafeOldDir, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, cleanupStaleBufferedResponseSpoolDirs(cleanupBase, time.Hour, time.Now()))
	_, err = os.Stat(unsafeOldDir)
	require.NoError(t, err, "cleanup must preserve a directory whose private security cannot be proven")
}
