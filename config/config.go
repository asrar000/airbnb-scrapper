package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application-level configuration.
type Config struct {
	DB      DatabaseConfig
	Scraper ScraperConfig
	Airbnb  AirbnbConfig
	Output  OutputConfig
}

// DatabaseConfig contains PostgreSQL connection parameters.
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

// DSN returns a PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name,
	)
}

// ScraperConfig holds all chromedp and rate-limiting parameters.
type ScraperConfig struct {
	ListingsPerPage    int
	PagesToScrape      int
	PageLoadTimeout    time.Duration
	ActionTimeout      time.Duration
	RateLimit          time.Duration
	RandomDelay        time.Duration
	MaxRetries         int
	Headless           bool
}

// AirbnbConfig holds Airbnb target configuration.
type AirbnbConfig struct {
	SearchLocation string
}

// OutputConfig holds output paths.
type OutputConfig struct {
	CSVPath string
}

// Load reads .env and returns a populated Config.
func Load() (*Config, error) {
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
			ListingsPerPage: getEnvInt("LISTINGS_PER_PAGE", 5),
			PagesToScrape:   getEnvInt("PAGES_TO_SCRAPE", 2),
			PageLoadTimeout: time.Duration(getEnvInt("PAGE_LOAD_TIMEOUT_SEC", 60)) * time.Second,
			ActionTimeout:   time.Duration(getEnvInt("ACTION_TIMEOUT_SEC", 30)) * time.Second,
			RateLimit:       time.Duration(getEnvInt("RATE_LIMIT_MS", 4000)) * time.Millisecond,
			RandomDelay:     time.Duration(getEnvInt("RANDOM_DELAY_MS", 3000)) * time.Millisecond,
			MaxRetries:      getEnvInt("MAX_RETRIES", 3),
			Headless:        getEnvBool("HEADLESS", true),
		},
		Airbnb: AirbnbConfig{
			SearchLocation: getEnv("AIRBNB_SEARCH_LOCATION", "Kuala Lumpur"),
		},
		Output: OutputConfig{
			CSVPath: getEnv("OUTPUT_CSV", "./output/raw_listings.csv"),
		},
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

func getEnvBool(key string, fallback bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return fallback
}