//go:build ee

package observability

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const activeUsersKey = "active_users"

const ActiveUsersWindow = 5 * time.Minute

// MarkUserActive refreshes a user's last heartbeat in the shared Redis set.
func MarkUserActive(ctx context.Context, client *redis.Client, userID string) error {
	if client == nil {
		return fmt.Errorf("active users: Redis is not initialized")
	}
	if userID == "" {
		return fmt.Errorf("active users: user ID is required")
	}
	if err := client.ZAdd(ctx, activeUsersKey, redis.Z{Score: float64(time.Now().Unix()), Member: userID}).Err(); err != nil {
		return fmt.Errorf("mark user active: %w", err)
	}
	return nil
}

// ActiveUsersCount removes heartbeats at or before the cutoff and counts the rest.
func ActiveUsersCount(ctx context.Context, client *redis.Client, window time.Duration) (int64, error) {
	if client == nil {
		return 0, fmt.Errorf("active users: Redis is not initialized")
	}
	if window <= 0 {
		return 0, fmt.Errorf("active users: window must be positive")
	}
	cutoff := time.Now().Unix() - int64(window/time.Second)
	// Remove expired users and count in one transaction, shared across API workers.
	pipe := client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, activeUsersKey, "0", strconv.FormatInt(cutoff, 10))
	count := pipe.ZCard(ctx, activeUsersKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("count active users: %w", err)
	}
	return count.Val(), nil
}
