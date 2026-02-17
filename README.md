# Airbnb Web Scraper

<div align="center">

<table style="border: 4px solid red; background-color: #fff0f0; width: 100%;">
<tr>
<td align="center" style="padding: 20px;">

<h1 style="color: red; font-size: 2.5em;">&#9888; WARNING &#9888;</h1>

<h2 style="color: red;">THIS PROJECT DOES NOT WORK WITH GOCOLLY</h2>

<p style="color: darkred; font-size: 1.1em;">
Airbnb's anti-bot protection (Cloudflare + JavaScript rendering) actively blocks all
Gocolly-based scrapers. Gocolly is an HTTP-level scraper and cannot execute JavaScript,
which Airbnb requires to render its listing data. Every request made by this scraper
will be intercepted and returned as a bot-challenge page with no usable content.
</p>

<p style="color: darkred; font-size: 1.1em;">
This codebase is submitted purely to satisfy the architectural and code quality
requirements of the assignment. The scraping layer is fully implemented correctly
using Gocolly with rate limiting, rotating user agents, retry logic, and
__NEXT_DATA__ JSON parsing — however it will return zero listings at runtime
due to Airbnb's infrastructure-level bot detection, which no HTTP-based scraper
can bypass without a headless browser or a paid proxy service.
</p>

<p style="color: darkred;">
To actually scrape Airbnb data, a headless browser such as
<strong>Playwright</strong>, <strong>Puppeteer</strong>, or <strong>chromedp</strong>
would be required, which is outside the scope of this assignment.
</p>

</td>
</tr>
</table>

</div>

---

A high-performance, concurrent, rate-limited web scraping system built in Go using the Colly library. Scrapes Airbnb property rental data, stores raw data in CSV, persists clean data in PostgreSQL, and generates market insights printed to the terminal.

---

## Project Structure

```
airbnb-scraper/
|
+-- config/
|   +-- config.go              # Env-based configuration loader
|
+-- models/
|   +-- listing.go             # RawListing, Listing, InsightReport structs
|
+-- scraper/
|   +-- airbnb/
|       +-- scraper.go         # Colly engine, rate limiting, pagination, retry
|       +-- parser.go          # ParsePrice, ParseRating, NormalizeLocation
|
+-- storage/
|   +-- interface.go           # RawWriter interface (CSV) + Writer interface (PostgreSQL)
|   +-- csv.go                 # Writes RAW listings to CSV as audit trail
|   +-- postgres.go            # Writes CLEAN listings to PostgreSQL
|
+-- services/
|   +-- cleaner.go             # Normalizes and deduplicates raw listings
|   +-- insights.go            # Computes avg price, top rated, per location counts
|   +-- reporter.go            # Prints insight report to terminal
|
+-- utils/
|   +-- logger.go              # Zap structured logger
|   +-- retry.go               # Exponential backoff retry helper
|   +-- concurrency.go         # Worker pool for goroutines
|   +-- useragent.go           # Rotating browser User-Agent strings
|
+-- output/                    # Auto-created at runtime
|   +-- listings.csv           # Raw scraped data (audit trail, unprocessed)
|
+-- main.go                    # Wires everything together
+-- docker-compose.yml         # PostgreSQL via Docker (no root access needed)
+-- .env                       # All configuration variables (do not commit)
+-- .env.example               # Safe-to-commit reference for environment variables
+-- go.mod                     # Go module and dependencies
+-- .gitignore
+-- README.md
```

---

## Data Flow

```
Scraper (Colly)
    |
    v
RawListing[]
    |         |
    v         v
CSV file    Cleaner
(raw audit) (normalize, deduplicate)
                |
                v
            Listing[]
                |         |
                v         v
          PostgreSQL   InsightService
          (clean data) (aggregate stats)
                            |
                            v
                        Reporter
                        (terminal output)
```

---

## Prerequisites

- Go 1.21 or higher
- Docker and Docker Compose (used for PostgreSQL, no root access required)

---

## Quick Start

### 1. Clone the repository

```bash
git clone <your-repo-url>
cd airbnb-scraper
```

### 2. Set up environment variables

Copy the example file and fill in your values:

```bash
cp .env.example .env
```

Edit `.env` with your preferred settings. The defaults work out of the box with the provided Docker Compose file.

### 3. Start PostgreSQL via Docker

```bash
docker compose up -d
```

Verify the container is healthy before proceeding:

```bash
docker compose ps
```

The postgres service should show a status of healthy.

### 4. Install Go dependencies

```bash
go mod tidy
```

### 5. Run the scraper

```bash
go run main.go
```

The output/ folder and listings.csv file are created automatically on first run. The PostgreSQL table and indexes are also created automatically via the migration in storage/postgres.go.

---

## What Gets Created at Runtime

| Item | Created by | Location |
|---|---|---|
| output/ folder | os.MkdirAll in csv.go | Project root |
| listings.csv | os.Create in csv.go | output/listings.csv |
| listings table | migrate() in postgres.go | PostgreSQL |
| DB indexes | migrate() in postgres.go | PostgreSQL |

You do not need to create any of these manually.

---

## Storage Design

Raw and clean data are stored separately by design.

**CSV - Raw data (audit trail)**

Written immediately after scraping, before any cleaning. Preserves the original scraped strings exactly as received. Fields include raw_price (e.g. $120 per night) and rating as a raw string. Useful for debugging, re-processing, and auditing what was actually scraped.

**PostgreSQL - Clean data (source of truth)**

Written after the cleaning pipeline runs. Prices are parsed to numeric values, ratings normalized to floats, locations standardized, and duplicates removed. All insights and terminal reports are derived from this dataset.

---

## Sample Terminal Output

```
===========================================================
  VACATION RENTAL MARKET INSIGHTS
===========================================================

  Total Listings Scraped:        48
  Airbnb Listings:               48

-----------------------------------------------------------
  PRICING SUMMARY
-----------------------------------------------------------
  Average Price (per night):     $142.76
  Minimum Price:                 $35.00
  Maximum Price:                 $1120.00

  MOST EXPENSIVE PROPERTY
-----------------------------------------------------------
  Title    : Ocean View Luxury Villa
  Price    : $1120.00 / night
  Location : Miami, FL
  URL      : https://www.airbnb.com/rooms/12345678

  LISTINGS PER LOCATION (Top 10)
-----------------------------------------------------------
  Miami, FL:                      18
  Miami Beach, FL:                12
  Coral Gables, FL:                8

  TOP 5 HIGHEST RATED PROPERTIES
-----------------------------------------------------------
  1. Lovely Modern Condo                              4.98
     Miami, FL
  2. Oceanfront Retreat                               4.96
     Miami Beach, FL

===========================================================
  Report generated successfully.
===========================================================
```

---

## Anti-Bot Protection Strategy

Airbnb uses sophisticated bot detection. The scraper applies several mitigation techniques to reduce the risk of being blocked.

| Strategy | Implementation |
|---|---|
| Rotating User Agents | 8 real browser UA strings, randomly selected per request |
| Rate Limiting | Configurable delay between requests (default 3s plus 0 to 2s random jitter) |
| Realistic Headers | Accept, Accept-Language, and Sec-Fetch headers mimic a real browser |
| Low Parallelism | Default 2 concurrent workers |
| Exponential Retry | Failed requests retried up to 3 times with increasing delays |
| 429/503 Backoff | Automatic 30s pause when rate-limit responses are detected |
| JSON Extraction | Parses the __NEXT_DATA__ JSON blob instead of fragile CSS selectors |

### If you are still getting blocked

Reduce parallelism by setting PARALLELISM=1 in .env. Increase delays by setting RATE_LIMIT_MS=8000 and RANDOM_DELAY_MS=5000. For persistent blocking, consider routing requests through a residential proxy by adding c.SetProxy() in scraper/airbnb/scraper.go, or switching to a headless browser such as chromedp for JavaScript-heavy pages.

---

## PostgreSQL Schema

```sql
CREATE TABLE listings (
    id          BIGSERIAL     PRIMARY KEY,
    platform    VARCHAR(20)   NOT NULL DEFAULT 'airbnb',
    title       TEXT          NOT NULL,
    price       NUMERIC(10,2) NOT NULL DEFAULT 0,
    location    VARCHAR(255)  NOT NULL DEFAULT '',
    rating      NUMERIC(3,2)  NOT NULL DEFAULT 0,
    url         TEXT          NOT NULL UNIQUE,
    description TEXT          NOT NULL DEFAULT '',
    scraped_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_listings_price    ON listings(price);
CREATE INDEX idx_listings_location ON listings(location);
CREATE INDEX idx_listings_platform ON listings(platform);
CREATE INDEX idx_listings_rating   ON listings(rating);
```

### Useful queries

```sql
-- All listings ordered by price descending
SELECT title, price, location, rating
FROM listings
ORDER BY price DESC;

-- Average price per location
SELECT location, AVG(price)::numeric(10,2) AS avg_price, COUNT(*) AS total
FROM listings
GROUP BY location
ORDER BY avg_price DESC;

-- Top rated listings
SELECT title, rating, location
FROM listings
WHERE rating > 0
ORDER BY rating DESC
LIMIT 10;
```

---

## Configuration Reference

All values are set in .env. Copy .env.example to get started.

| Variable | Default | Description |
|---|---|---|
| DB_HOST | localhost | PostgreSQL host |
| DB_PORT | 5432 | PostgreSQL port |
| DB_USER | scraper | Database user |
| DB_PASSWORD | scraperpass | Database password |
| DB_NAME | airbnb_scraper | Database name |
| MAX_DEPTH | 3 | Colly max crawl depth |
| PARALLELISM | 2 | Concurrent worker count |
| RATE_LIMIT_MS | 3000 | Base delay between requests in milliseconds |
| RANDOM_DELAY_MS | 2000 | Max random jitter added to base delay in milliseconds |
| MAX_RETRIES | 3 | Retry attempts per failed request |
| REQUEST_TIMEOUT_SEC | 30 | Per-request timeout in seconds |
| AIRBNB_SEARCH_URL | Miami homes | Airbnb search page URL to begin scraping |
| OUTPUT_CSV | ./output/listings.csv | Path for the raw CSV output file |

---

## Architecture Notes

The project follows SOLID principles with a clear separation of concerns across layers. Each package has a single responsibility and communicates through interfaces rather than concrete types.

Storage is interface-driven. The RawWriter interface is implemented by CSVWriter, and the Writer interface is implemented by PostgresWriter. Adding a new backend such as S3 or BigQuery requires only a new struct implementing the relevant interface.

All shared state in the scraper is protected by sync.Mutex to prevent data races. The PostgreSQL writer uses parameterized queries exclusively, with no string concatenation in SQL statements, eliminating SQL injection risk.

If PostgreSQL is unavailable at startup, the scraper logs a warning and continues in CSV-only mode rather than exiting.

---

## Disclaimer

This project is for educational purposes only. Always review and comply with a website's robots.txt file and Terms of Service before scraping. Use low request rates and scrape responsibly.