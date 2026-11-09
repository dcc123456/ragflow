//go:build ee

package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestActiveUsersSharedCountAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	other := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close(); other.Close() })
	ctx := t.Context()
	now := time.Now().Unix()
	if err := client.ZAdd(ctx, activeUsersKey,
		redis.Z{Member: "expired", Score: float64(now - 1000)},
		redis.Z{Member: "cutoff", Score: float64(now - 300)},
		redis.Z{Member: "returning", Score: float64(now - 1000)},
	).Err(); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"returning", "new", "new"} {
		if err := MarkUserActive(ctx, other, user); err != nil {
			t.Fatal(err)
		}
	}
	count, err := ActiveUsersCount(ctx, client, ActiveUsersWindow)
	if err != nil || count != 2 {
		t.Fatalf("count = %d, error = %v; want 2", count, err)
	}
	if _, err := client.ZScore(ctx, activeUsersKey, "cutoff").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("cutoff entry was not removed: %v", err)
	}
	if _, err := client.ZScore(ctx, activeUsersKey, "expired").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("expired entry was not removed: %v", err)
	}
	if err := client.ZAdd(ctx, activeUsersKey, redis.Z{Member: "older", Score: float64(now - 100)}).Err(); err != nil {
		t.Fatal(err)
	}
	count, err = ActiveUsersCount(ctx, client, time.Minute)
	if err != nil || count != 2 {
		t.Fatalf("custom window count = %d, error = %v; want 2", count, err)
	}
}

func TestActiveUsersErrors(t *testing.T) {
	ctx := t.Context()
	if err := MarkUserActive(ctx, nil, "user"); err == nil {
		t.Fatal("nil Redis client was accepted")
	}
	if _, err := ActiveUsersCount(ctx, nil, ActiveUsersWindow); err == nil {
		t.Fatal("nil Redis client was accepted")
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	if err := MarkUserActive(ctx, client, ""); err == nil {
		t.Fatal("empty user ID was accepted")
	}
	if _, err := ActiveUsersCount(ctx, client, 0); err == nil {
		t.Fatal("non-positive window was accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := MarkUserActive(canceled, client, "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	if _, err := ActiveUsersCount(canceled, client, ActiveUsersWindow); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	if err := client.Set(ctx, activeUsersKey, "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := MarkUserActive(ctx, client, "user"); err == nil {
		t.Fatal("Redis write error was ignored")
	}
	if _, err := ActiveUsersCount(ctx, client, ActiveUsersWindow); err == nil {
		t.Fatal("Redis read error was ignored")
	}
}
