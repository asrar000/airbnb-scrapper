#  Airbnb Web Scraper

A high-performance, concurrent, rate-limited web scraping system built in **Go** using the **Colly** library. Scrapes Airbnb property rental data, stores it in PostgreSQL, and generates market insights.

---

##  Project Structure

```
airbnb-scraper/
├── config/
│   └── config.go          # Env-based configuration loader
├── models/
│   └── listing.go         # RawListing, Listing, InsightReport DTOs
├── scraper/
│   └── airbnb/
│       ├── scraper.go     # Core Colly scraper with rate limiting
│       └── parser.go      # Price/rating/location normalizers
├── storage/
│   ├── interface.go       # Writer interface
│   ├── postgres.go        # PostgreSQL batch writer
│   └── csv.go             # CSV fallback writer
├── services/
│   ├── cleaner.go         # Data normalization & deduplication
│   ├── insights.go        # Analytics engine
│   └── reporter.go        # Terminal report printer
├── utils/
│   ├── logger.go          # Zap structured logger
│   ├── retry.go           # Exponential backoff retry
│   ├── concurrency.go     # Worker pool
│   └── useragent.go       # Rotating user agents
├── output/                # CSV output directory (auto-created)
├── main.go                # Composition root
├── docker-compose.yml     # PostgreSQL via Docker
├── .env                   # Environment configuration
└── README.md
```

---

##  Prerequisites

- **Go 1.21+**
- **Docker + Docker Compose** (for PostgreSQL — no root needed)

---

##  Quick Start

### 1. Start PostgreSQL in Docker

```bash
docker compose up -d
```

Wait for the health check to pass:

```bash
docker compose ps
# postgres should show "healthy"
```

### 2. Install Go dependencies

```bash
go mod tidy
```

### 3. Configure environment

Edit `.env` to set your desired search URL and scraping parameters:

```env
# Change this to scrape a different city:
AIRBNB_SEARCH_URL=https://www.airbnb.com/s/New-York--NY--United-States/homes

# Increase delay if you're getting blocked:
RATE_LIMIT_MS=5000
RANDOM_DELAY_MS=3000
PARALLELISM=1
```

### 4. Run the scraper

```bash
go run main.go
```

---

##  Sample Terminal Output

```
═══════════════════════════════════════════════════════════
    VACATION RENTAL MARKET INSIGHTS
═══════════════════════════════════════════════════════════

  Total Listings Scraped:        48
  Airbnb Listings:               48

───────────────────────────────────────────────────────────
    PRICING SUMMARY
───────────────────────────────────────────────────────────
  Average Price (per night):     $142.76
  Minimum Price:                 $35.00
  Maximum Price:                 $1120.00

    MOST EXPENSIVE PROPERTY
───────────────────────────────────────────────────────────
  Title    : Ocean View Luxury Villa
  Price    : $1120.00 / night
  Location : Miami, FL

    TOP 5 HIGHEST RATED PROPERTIES
───────────────────────────────────────────────────────────
  1. Lovely Modern Condo                              4.98 ★★★★★
  2. Oceanfront Retreat                               4.96 ★★★★★
```

---

##  Anti-Bot Protection Strategy

Airbnb uses sophisticated bot detection. The scraper employs several mitigation strategies:

| Strategy | Implementation |
|---|---|
| **Rotating User Agents** | 8 real browser UA strings, randomly selected per request |
| **Rate Limiting** | Configurable delay between requests (default 3s + 0–2s random jitter) |
| **Realistic Headers** | Accept, Accept-Language, Sec-Fetch-* headers mimic real browsers |
| **Low Parallelism** | Default 2 concurrent workers (reduce to 1 if getting blocked) |
| **Exponential Retry** | Failed requests are retried up to 3× with increasing delays |
| **429/503 Backoff** | Automatic 30s pause when rate-limit responses are detected |
| **JSON Extraction** | Parses `__NEXT_DATA__` JSON blob instead of fragile CSS selectors |

### If you're still getting blocked:

1. **Reduce parallelism** — set `PARALLELISM=1` in `.env`
2. **Increase delays** — try `RATE_LIMIT_MS=8000` and `RANDOM_DELAY_MS=5000`
3. **Use a residential proxy** — add proxy support in `scraper.go` via `c.SetProxy()`
4. **Use a headless browser** — consider `chromedp` for JavaScript-heavy pages
5. **Use the official Airbnb API** — if available for your use case

---

## 🗄️ PostgreSQL Schema

```sql
CREATE TABLE listings (
    id          BIGSERIAL PRIMARY KEY,
    platform    VARCHAR(20)   NOT NULL DEFAULT 'airbnb',
    title       TEXT          NOT NULL,
    price       NUMERIC(10,2) NOT NULL DEFAULT 0,
    location    VARCHAR(255)  NOT NULL DEFAULT '',
    rating      NUMERIC(3,2)  NOT NULL DEFAULT 0,
    url         TEXT          NOT NULL UNIQUE,
    description TEXT          NOT NULL DEFAULT '',
    scraped_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_listings_price    ON listings(price);
CREATE INDEX idx_listings_location ON listings(location);
CREATE INDEX idx_listings_platform ON listings(platform);
CREATE INDEX idx_listings_rating   ON listings(rating);
```

### Useful queries:

```sql
-- All listings ordered by price
SELECT title, price, location, rating FROM listings ORDER BY price DESC;

-- Average price by location
SELECT location, AVG(price)::numeric(10,2) AS avg_price, COUNT(*) AS total
FROM listings GROUP BY location ORDER BY avg_price DESC;

-- Top rated
SELECT title, rating, location FROM listings WHERE rating > 0 ORDER BY rating DESC LIMIT 10;
```

---

##  Configuration Reference

| Variable | Default | Description |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `scraper` | Database user |
| `DB_PASSWORD` | `scraperpass` | Database password |
| `DB_NAME` | `airbnb_scraper` | Database name |
| `MAX_DEPTH` | `3` | Colly max crawl depth |
| `PARALLELISM` | `2` | Concurrent workers |
| `RATE_LIMIT_MS` | `3000` | Base delay between requests (ms) |
| `RANDOM_DELAY_MS` | `2000` | Max random jitter added to delay (ms) |
| `MAX_RETRIES` | `3` | Retry attempts on failure |
| `AIRBNB_SEARCH_URL` | Miami homes | Search page to scrape |
| `OUTPUT_CSV` | `./output/listings.csv` | CSV output path |

---

##  Architecture Highlights

- **SOLID principles** — each struct has a single responsibility; storage, cleaning, insights, and reporting are fully decoupled
- **Interface-driven storage** — `Writer` interface makes it trivial to add new backends (S3, BigQuery, etc.)
- **Thread-safe** — all shared state is protected by `sync.Mutex`; no data races
- **No SQL injection** — all DB queries use parameterized statements
- **Graceful degradation** — if PostgreSQL is down, falls back to CSV-only mode

---

## Disclaimer
Always respect a website's `robots.txt` and Terms of Service. Scrape responsibly with low request rates.