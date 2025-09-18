package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Singleton Redis client
var rdb *redis.Client

func InitRedis() {
	addr := os.Getenv("REDIS_ADDR")
	pass := os.Getenv("REDIS_PASS")
	db := 0

	// Provide default values if environment variables are not set
	if addr == "" {
		addr = "localhost:6379"
		log.Printf("REDIS_ADDR not set, using default: %s", addr)
	}

	if pass == "" {
		log.Printf("REDIS_PASS not set, connecting without password")
	}

	log.Printf("Connecting to Redis at: %s", addr)

	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pass,
		DB:       db,
	})
}

// PushOrderbook stores the orderbook in Redis under the right key
func PushOrderbook(ctx context.Context, ob NormalizationSchema) error {
	if rdb == nil {
		return fmt.Errorf("Redis client not initialized, call InitRedis() first")
	}

	key := fmt.Sprintf("orderbook:%s:%s", ob.Exchange, ob.Pair)

	// serialize to JSON
	data, err := json.Marshal(ob)
	if err != nil {
		return err
	}

	// write with TTL (optional). Here: 30 seconds
	err = rdb.Set(ctx, key, data, 30*time.Second).Err()
	if err != nil {
		return err
	}

	err = rdb.Publish(ctx, "orderbook_updates", key).Err()
	if err != nil {
		return err
	}

	log.Printf("Pushed and Published orderbook to Redis: %s", key)
	return nil
}

func GetFromOrderBook(ctx context.Context, key string) (NormalizationSchema, error) {
	if rdb == nil {
		return NormalizationSchema{}, fmt.Errorf("Redis client not initialized, call InitRedis() first")
	}

	data, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return NormalizationSchema{}, err
	}

	var ob NormalizationSchema
	err = json.Unmarshal([]byte(data), &ob)
	if err != nil {
		return NormalizationSchema{}, err
	}

	return ob, nil
}

// TestRedisConnection tests if Redis connection is working
func TestRedisConnection() error {
	if rdb == nil {
		return fmt.Errorf("Redis client not initialized, call InitRedis() first")
	}

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("Redis connection failed: %v", err)
	}

	return nil
}
