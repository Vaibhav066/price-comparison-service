package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"price-comparison-api/internal/models"
)

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
	ctx    context.Context
}

func NewRedisCache() *RedisCache {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	redisDB := 0
	if db := os.Getenv("REDIS_DB"); db != "" {
		if dbNum, err := strconv.Atoi(db); err == nil {
			redisDB = dbNum
		}
	}

	ttlSeconds := 600 // 10 minutes default
	if ttl := os.Getenv("CACHE_TTL"); ttl != "" {
		if t, err := strconv.Atoi(ttl); err == nil {
			ttlSeconds = t
		}
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("Failed to parse Redis URL: %v", err)
		return nil
	}

	// Use the redisDB variable here
	opt.DB = redisDB

	client := redis.NewClient(opt)
	ctx := context.Background()

	// Test connection
	_, err = client.Ping(ctx).Result()
	if err != nil {
		log.Printf("Redis connection failed: %v", err)
		return nil
	}

	log.Printf("Redis connected successfully, DB: %d, TTL: %d seconds", redisDB, ttlSeconds)

	return &RedisCache{
		client: client,
		ttl:    time.Duration(ttlSeconds) * time.Second,
		ctx:    ctx,
	}
}

func (r *RedisCache) GetSearchResults(key string) (*models.SearchResponse, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("redis client not available")
	}

	val, err := r.client.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Cache miss
	}
	if err != nil {
		return nil, fmt.Errorf("redis get error: %v", err)
	}

	var response models.SearchResponse
	err = json.Unmarshal([]byte(val), &response)
	if err != nil {
		return nil, fmt.Errorf("json unmarshal error: %v", err)
	}

	return &response, nil
}

func (r *RedisCache) SetSearchResults(key string, response *models.SearchResponse) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis client not available")
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("json marshal error: %v", err)
	}

	return r.client.Set(r.ctx, key, data, r.ttl).Err()
}

func (r *RedisCache) GenerateSearchKey(params models.SearchParams) string {
	key := fmt.Sprintf("search:%s:%s:p%d:l%d", params.Query, params.Country, params.Page, params.Limit)

	if params.Filters != nil {
		if params.Filters.MinPrice > 0 {
			key += fmt.Sprintf(":minp%.2f", params.Filters.MinPrice)
		}
		if params.Filters.MaxPrice > 0 {
			key += fmt.Sprintf(":maxp%.2f", params.Filters.MaxPrice)
		}
		if params.Filters.Source != "" {
			key += fmt.Sprintf(":src%s", params.Filters.Source)
		}
		if params.Filters.InStock != nil {
			key += fmt.Sprintf(":stock%t", *params.Filters.InStock)
		}
		if params.Filters.MinRating > 0 {
			key += fmt.Sprintf(":rating%.1f", params.Filters.MinRating)
		}
	}

	if params.Sort != nil {
		key += fmt.Sprintf(":sort%s:%s", params.Sort.Field, params.Sort.Order)
	}

	return key
}

func (r *RedisCache) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *RedisCache) IsAvailable() bool {
	return r != nil && r.client != nil
}

func (r *RedisCache) GetStats() map[string]interface{} {
	if r == nil || r.client == nil {
		return map[string]interface{}{
			"status": "unavailable",
		}
	}

	info := r.client.Info(r.ctx, "memory").Val()
	return map[string]interface{}{
		"status":      "connected",
		"ttl_seconds": int(r.ttl.Seconds()),
		"memory_info": info,
	}
}

func (r *RedisCache) GetAllKeys() []string {
	if r == nil || r.client == nil {
		return []string{}
	}
	keys, err := r.client.Keys(r.ctx, "search:*").Result()
	if err != nil {
		return []string{}
	}
	return keys
}

func (r *RedisCache) FlushCache() error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis client not available")
	}
	return r.client.FlushDB(r.ctx).Err()
}

func (r *RedisCache) GetKeyTTL(key string) time.Duration {
	if r == nil || r.client == nil {
		return 0
	}
	ttl, err := r.client.TTL(r.ctx, key).Result()
	if err != nil {
		return 0
	}
	return ttl
}
