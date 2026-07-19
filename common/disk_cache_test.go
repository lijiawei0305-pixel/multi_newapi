package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureDiskCacheDirUsesPrivatePermissions(t *testing.T) {
	original := GetDiskCacheConfig()
	t.Cleanup(func() { SetDiskCacheConfig(original) })
	base := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{Path: base})

	require.NoError(t, EnsureDiskCacheDir())
	info, err := os.Stat(filepath.Join(base, diskCacheDir))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())
}
