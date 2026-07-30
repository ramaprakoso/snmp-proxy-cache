package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"snmp-proxy-cache/internal/config"

	"github.com/redis/go-redis/v9"
)

// CachedEntry holds cached SNMP OID value and data type metadata.
type CachedEntry struct {
	DeviceID    string    `json:"device_id"`
	OID         string    `json:"oid"`
	Value       string    `json:"value"`
	DataType    string    `json:"data_type"`
	CollectedAt time.Time `json:"collected_at"`
}

// RedisStore handles Redis operations for SNMP Cache.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStore initializes Go-Redis client.
func NewRedisStore(cfg *config.Config) (*RedisStore, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		opts = &redis.Options{
			Addr:     "localhost:6379",
			Password: cfg.RedisPassword,
		}
	}
	if cfg.RedisPassword != "" {
		opts.Password = cfg.RedisPassword
	}

	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisStore{
		client: rdb,
		ttl:    time.Duration(cfg.CacheTTLSec) * time.Second,
	}, nil
}

// GetCachedVarBind retrieves cached entry for a device and OID.
func (rs *RedisStore) GetCachedVarBind(ctx context.Context, deviceID, oid string) (*CachedEntry, bool) {
	key := fmt.Sprintf("snmp:val:%s:%s", deviceID, oid)
	valStr, err := rs.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}

	var entry CachedEntry
	if err := json.Unmarshal([]byte(valStr), &entry); err != nil {
		return nil, false
	}

	return &entry, true
}

// SetCachedVarBind stores polled OID result into Redis with TTL.
func (rs *RedisStore) SetCachedVarBind(ctx context.Context, deviceID, oid, value, dataType string) error {
	key := fmt.Sprintf("snmp:val:%s:%s", deviceID, oid)
	entry := CachedEntry{
		DeviceID:    deviceID,
		OID:         oid,
		Value:       value,
		DataType:    dataType,
		CollectedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return rs.client.Set(ctx, key, string(data), rs.ttl).Err()
}

// GetOIDRegistry retrieves the union set of all OIDs registered for a device.
func (rs *RedisStore) GetOIDRegistry(ctx context.Context, deviceID string) ([]string, error) {
	key := fmt.Sprintf("snmp:registry:%s", deviceID)
	return rs.client.SMembers(ctx, key).Result()
}

// AddToOIDRegistry adds OIDs to the device's union registry.
func (rs *RedisStore) AddToOIDRegistry(ctx context.Context, deviceID string, oids ...string) error {
	if len(oids) == 0 {
		return nil
	}
	key := fmt.Sprintf("snmp:registry:%s", deviceID)
	args := make([]interface{}, len(oids))
	for i, o := range oids {
		args[i] = o
	}
	return rs.client.SAdd(ctx, key, args...).Err()
}

// ResetOIDRegistry wipes old OID registry and initializes a new clean union registry.
func (rs *RedisStore) ResetOIDRegistry(ctx context.Context, deviceID string, newOIDs []string) error {
	key := fmt.Sprintf("snmp:registry:%s", deviceID)
	_ = rs.client.Del(ctx, key).Err()
	if len(newOIDs) > 0 {
		args := make([]interface{}, len(newOIDs))
		for i, o := range newOIDs {
			args[i] = o
		}
		return rs.client.SAdd(ctx, key, args...).Err()
	}
	return nil
}

// Close closes Redis pool.
func (rs *RedisStore) Close() error {
	return rs.client.Close()
}
