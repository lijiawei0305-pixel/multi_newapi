package migrate_test

import (
	"context"
	paymentmigrate "github.com/QuantumNous/new-api/internal/payment/migrate"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestEnsureSchemaTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, paymentmigrate.EnsureSchema(context.Background(), db))
	require.NoError(t, paymentmigrate.EnsureSchema(context.Background(), db))
	v, err := paymentmigrate.CurrentVersion(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, paymentmigrate.SchemaVersion, v)
	require.NoError(t, paymentmigrate.VerifyOnly(context.Background(), db))
}
