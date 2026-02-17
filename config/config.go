package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application-level configuration loaded from environment variables.
type Config struct {
	DB        DatabaseConfig
	Scraper   ScraperConfig
	Airbnb    AirbnbConfig
	OutputCSV string
}

// DatabaseConfig contains PostgreSQL connection parameters.
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

// DSN returns a PostgreSQL data source name string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name,
	)
}

// ScraperConfig contains all scraper tuning parameters.
type ScraperConfig struct {
	MaxDepth          int
	Parallelism       int
	RateLimit         time.Duration
	RandomDelay       time.Duration
	MaxRetries        int
	RequestTimeoutSec int
}

// AirbnbConfig contains Airbnb-specific URLs.
type AirbnbConfig struct {
	BaseURL   string
	SearchURL string
}

// Load reads .env and returns a populated Config.
func Load() (*Config, error) {
	// Best-effort load: if .env is missing in production, rely on real env vars
	_ = godotenv.Load()

	cfg := &Config{
		DB: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "scraper"),
			Password: getEnv("DB_PASSWORD", "scraperpass"),
			Name:     getEnv("DB_NAME", "airbnb_scraper"),
		},
		Scraper: ScraperConfig{
			MaxDepth:          getEnvInt("MAX_DEPTH", 3),
			Parallelism:       getEnvInt("PARALLELISM", 2),
			RateLimit:         time.Duration(getEnvInt("RATE_LIMIT_MS", 3000)) * time.Millisecond,
			RandomDelay:       time.Duration(getEnvInt("RANDOM_DELAY_MS", 2000)) * time.Millisecond,
			MaxRetries:        getEnvInt("MAX_RETRIES", 3),
			RequestTimeoutSec: getEnvInt("REQUEST_TIMEOUT_SEC", 30),
		},
		Airbnb: AirbnbConfig{
			BaseURL:   getEnv("AIRBNB_BASE_URL", "https://www.airbnb.com"),
			SearchURL: getEnv("AIRBNB_SEARCH_URL", "https://www.airbnb.com/s/Miami--FL--United-States/homes"),
		},
		OutputCSV: getEnv("OUTPUT_CSV", "./output/listings.csv"),
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}