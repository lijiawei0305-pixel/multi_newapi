package risk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemKV_IncrSequential(t *testing.T) {
	kv := NewMemKVCache(nil)
	ctx := context.Background()
	for want := int64(1); want <= 3; want++ {
		got, err := kv.Incr(ctx, "k")
		if err != nil || got != want {
			t.Fatalf("Incr = %d (err=%v), want %d", got, err, want)
		}
	}
}

func TestMemKV_DecrFloorsAtZeroAndDeletesAtomically(t *testing.T) {
	kv := NewMemKVCache(nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := kv.Incr(ctx, "purchase")
		require.NoError(t, err)
	}

	n, err := kv.Decr(ctx, "purchase")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	_, found, err := kv.Get(ctx, "purchase")
	require.NoError(t, err)
	assert.True(t, found)

	n, err = kv.Decr(ctx, "purchase")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	n, err = kv.Decr(ctx, "purchase")
	require.NoError(t, err)
	assert.Zero(t, n)
	_, found, err = kv.Get(ctx, "purchase")
	require.NoError(t, err)
	assert.False(t, found, "zero counter must be deleted")

	n, err = kv.Decr(ctx, "purchase")
	require.NoError(t, err)
	assert.Zero(t, n, "missing counter must not become negative")
}

func TestMemKV_GetMissingAndPresent(t *testing.T) {
	kv := NewMemKVCache(nil)
	ctx := context.Background()
	if _, found, _ := kv.Get(ctx, "missing"); found {
		t.Fatal("missing key should not be found")
	}
	_, _ = kv.SetNX(ctx, "k", "v", 0)
	val, found, err := kv.Get(ctx, "k")
	if err != nil || !found || val != "v" {
		t.Fatalf("Get = (%q,%v,%v), want (v,true,nil)", val, found, err)
	}
}

func TestMemKV_SetNXOnlyOnce(t *testing.T) {
	kv := NewMemKVCache(nil)
	ctx := context.Background()
	ok, _ := kv.SetNX(ctx, "k", "first", 0)
	if !ok {
		t.Fatal("first SetNX should succeed")
	}
	ok, _ = kv.SetNX(ctx, "k", "second", 0)
	if ok {
		t.Fatal("second SetNX should fail (key exists)")
	}
	val, _, _ := kv.Get(ctx, "k")
	if val != "first" {
		t.Fatalf("value = %q, want first (SetNX must not overwrite)", val)
	}
}

func TestMemKV_SetNXTTLExpiry(t *testing.T) {
	clk := newManualClock()
	kv := NewMemKVCache(clk)
	ctx := context.Background()
	ok, _ := kv.SetNX(ctx, "k", "v", time.Minute)
	if !ok {
		t.Fatal("SetNX should succeed")
	}
	// 未到期：仍占用。
	clk.Advance(30 * time.Second)
	if ok, _ := kv.SetNX(ctx, "k", "v2", time.Minute); ok {
		t.Fatal("key not yet expired; SetNX should fail")
	}
	// 过期后：可重新写入。
	clk.Advance(time.Minute)
	if ok, _ := kv.SetNX(ctx, "k", "v3", time.Minute); !ok {
		t.Fatal("key expired; SetNX should succeed")
	}
}

func TestMemKV_GetLazyExpiry(t *testing.T) {
	clk := newManualClock()
	kv := NewMemKVCache(clk)
	ctx := context.Background()
	_, _ = kv.SetNX(ctx, "k", "v", time.Minute)
	clk.Advance(2 * time.Minute)
	if _, found, _ := kv.Get(ctx, "k"); found {
		t.Fatal("expired key should not be found via Get")
	}
}

func TestMemKV_ExpireResetsCounterWindow(t *testing.T) {
	clk := newManualClock()
	kv := NewMemKVCache(clk)
	ctx := context.Background()
	_, _ = kv.Incr(ctx, "win")
	_, _ = kv.Incr(ctx, "win") // count = 2
	_ = kv.Expire(ctx, "win", time.Minute)
	clk.Advance(2 * time.Minute)
	// 过期后再次 Incr 从 1 重新计数。
	n, _ := kv.Incr(ctx, "win")
	if n != 1 {
		t.Fatalf("after expiry Incr = %d, want 1", n)
	}
}

func TestMemKV_ExpireMissingKeyNoop(t *testing.T) {
	kv := NewMemKVCache(nil)
	if err := kv.Expire(context.Background(), "nope", time.Minute); err != nil {
		t.Fatalf("Expire on missing key should be no-op, got %v", err)
	}
}

func TestMemKV_ExpireZeroClearsExpiry(t *testing.T) {
	clk := newManualClock()
	kv := NewMemKVCache(clk)
	ctx := context.Background()
	_, _ = kv.SetNX(ctx, "k", "v", time.Minute)
	// 清除过期：键变为永久。
	_ = kv.Expire(ctx, "k", 0)
	clk.Advance(time.Hour)
	if _, found, _ := kv.Get(ctx, "k"); !found {
		t.Fatal("key should persist after Expire(0) clears TTL")
	}
}

func TestMemKV_DefaultClockWhenNil(t *testing.T) {
	kv := NewMemKVCache(nil)
	if kv.clock == nil {
		t.Fatal("nil clock should default to systemClock")
	}
}
