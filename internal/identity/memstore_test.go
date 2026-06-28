package identity

import (
	"context"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

func TestMemTokenStorePutFind(t *testing.T) {
	s := NewMemTokenStore()
	s.Put("raw", TokenRecord{UserID: 5, TenantID: 6, Role: appctx.RoleUser})

	rec, found, err := s.FindByHash(context.Background(), HashToken("raw"))
	if err != nil || !found || rec == nil {
		t.Fatalf("expected hit: found=%v err=%v rec=%v", found, err, rec)
	}
	if rec.UserID != 5 || rec.TenantID != 6 {
		t.Fatalf("record mismatch: %+v", rec)
	}
	// 返回的应是副本：改动它不得污染内部 map。
	rec.UserID = 999
	again, _, _ := s.FindByHash(context.Background(), HashToken("raw"))
	if again.UserID != 5 {
		t.Fatal("FindByHash leaked internal state (no copy)")
	}

	if _, found, _ := s.FindByHash(context.Background(), HashToken("missing")); found {
		t.Fatal("missing token reported as found")
	}
}

func TestMemTenantStatusChecker(t *testing.T) {
	c := NewMemTenantStatusChecker()
	c.Set(1, TenantStatusActive)
	s, found, err := c.StatusOf(context.Background(), 1)
	if err != nil || !found || s != TenantStatusActive {
		t.Fatalf("unexpected: s=%v found=%v err=%v", s, found, err)
	}
	if _, found, _ := c.StatusOf(context.Background(), 42); found {
		t.Fatal("missing tenant reported as found")
	}
}

// -race 下验证内存假实现并发安全。
func TestMemStoresConcurrent(t *testing.T) {
	s := NewMemTokenStore()
	c := NewMemTenantStatusChecker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Put("k", TokenRecord{UserID: int64(i)})
			_, _, _ = s.FindByHash(context.Background(), HashToken("k"))
			c.Set(int64(i), TenantStatusActive)
			_, _, _ = c.StatusOf(context.Background(), int64(i))
		}(i)
	}
	wg.Wait()
}
