package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	envDatabaseURL       = "DATABASE_URL"
	envMinConns          = "DB_MIN_CONNS"
	envMaxConns          = "DB_MAX_CONNS"
	envMaxConnLifetime   = "DB_MAX_CONN_LIFETIME"
	envMaxConnIdleTime   = "DB_MAX_CONN_IDLE_TIME"
	envHealthCheckPeriod = "DB_HEALTH_CHECK_PERIOD"
)

type Config struct {
	DatabaseURL       string
	MinConns          int32
	MaxConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv(envDatabaseURL),
	}

	var err error
	if cfg.MinConns, err = loadOptionalInt32(envMinConns); err != nil {
		return Config{}, err
	}
	if cfg.MaxConns, err = loadOptionalInt32(envMaxConns); err != nil {
		return Config{}, err
	}
	if cfg.MaxConnLifetime, err = loadOptionalDuration(envMaxConnLifetime); err != nil {
		return Config{}, err
	}
	if cfg.MaxConnIdleTime, err = loadOptionalDuration(envMaxConnIdleTime); err != nil {
		return Config{}, err
	}
	if cfg.HealthCheckPeriod, err = loadOptionalDuration(envHealthCheckPeriod); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func NewPoolFromEnv(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return NewPool(ctx, cfg)
}

func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("%s is required", envDatabaseURL)
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	if cfg.MinConns > 0 {
		poolConfig.MinConns = cfg.MinConns
	}
	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func loadOptionalInt32(key string) (int32, error) {
	value := os.Getenv(key)
	if value == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return int32(parsed), nil
}

func loadOptionalDuration(key string) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return 0, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return duration, nil
}
