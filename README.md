# Airbnb Scraper

A concurrent, rate-limited web scraping system built in Go using chromedp (headless Chromium). Scrapes Airbnb property listings for a configured city, fetches detail pages for enriched data, stores raw output in CSV, persists clean data in PostgreSQL, and prints a market insights report to the terminal.

---

## Why chromedp and not Gocolly

Airbnb renders all listing data client-side via JavaScript. Gocolly is an HTTP-level scraper with no JavaScript engine, so it receives only an empty HTML shell and cannot see any listing cards. chromedp drives a real Chromium browser that fully executes JavaScript, which is the only reliable way to scrape Airbnb's search results.

---

## What it does

1. Opens a real Chromium browser (headless by default)
2. Navigates to Airbnb search results for the configured city (default: Kuala Lumpur)
3. Scrolls the page to trigger lazy-loading of listing cards
4. Extracts the first 5 listing cards from page 1
5. Navigates to each individual property page and extracts full details
6. Repeats for page 2 (first 5 listings)
7. Saves all 10 raw listings to CSV as an audit trail
8. Cleans and normalizes the data
9. Saves clean listings to PostgreSQL
10. Prints a market insights report to the terminal

---

## Project Structure

```
airbnb-scrapper/
|
+-- config/
|   +-- config.go              # Loads all settings from .env
|
+-- models/
|   +-- listing.go             # RawListing, Listing, InsightReport structs
|
+-- scraper/
|   +-- airbnb/
|       +-- scraper.go         # chromedp browser engine, pagination, detail fetcher
|       +-- parser.go          # ParsePrice, ParseRating, NormalizeLocation
|
+-- storage/
|   +-- interface.go           # RawWriter (CSV) and Writer (PostgreSQL) interfaces
|   +-- csv.go                 # Writes RAW listings to CSV
|   +-- postgres.go            # Writes CLEAN listings to PostgreSQL
|
+-- services/
|   +-- cleaner.go             # Normalizes and deduplicates raw listings
|   +-- insights.go            # Computes avg price, top rated, per-location counts
|   +-- reporter.go            # Prints insight report to terminal
|
+-- utils/
|   +-- logger.go              # Zap structured logger
|   +-- ratelimiter.go         # Manual rate limiter with random jitter (chromedp has none)
|   +-- retry.go               # Exponential backoff retry (chromedp has none)
|   +-- concurrency.go         # Worker pool for goroutines
|
+-- output/                    # Auto-created at runtime
|   +-- raw_listings.csv       # Raw scraped data, unprocessed
|
+-- main.go                    # Wires everything together (6-step pipeline)
+-- docker-compose.yml         # PostgreSQL via Docker, no root access needed
+-- .env                       # Configuration (do not commit)
+-- .env.example               # Safe reference copy of all config keys
+-- go.mod                     # Go module and dependencies
+-- .gitignore
+-- README.md
```

---

## Data Flow

```
chromedp (Chromium browser)
         |
         v
  Search Results Page
         |
         v
  Card URLs extracted (JS)
         |
         v
  Detail page per listing (chromedp navigates)
         |
         v
     RawListing[]
      |          |
      v          v
  CSV file     Cleaner
  (raw audit)  (normalize, deduplicate)
                   |
                   v
               Listing[]
                |       |
                v       v
          PostgreSQL  InsightService
          (clean)     (aggregate)
                           |
                           v
                       Reporter
                       (terminal)
```

---

## Manually Implemented Features

chromedp provides only raw browser automation. The following production-grade features are implemented manually in Go:

| Feature | File | How |
|---|---|---|
| Rate limiting | utils/ratelimiter.go | time.Duration ticker with minimum gap enforcement |
| Random jitter | utils/ratelimiter.go | rand.Int63n added on top of base delay |
| Retry with backoff | utils/retry.go | Exponential delay multiplied by attempt number |
| Worker pool | utils/concurrency.go | Goroutines reading from a buffered channel |
| Duplicate URL tracking | scraper/airbnb/scraper.go | sync.Mutex protected map[string]bool |
| Stealth JS injection | scraper/airbnb/scraper.go | navigator.webdriver overridden via chromedp.Evaluate |
| Popup dismissal | scraper/airbnb/scraper.go | chromedp.Click on known close button selectors |
| Lazy-load scroll | scraper/airbnb/scraper.go | JS window.scrollBy loop before card extraction |

---

## Prerequisites

- Go 1.22 or higher
- Google Chrome or Chromium installed on your machine
- Docker and Docker Compose (for PostgreSQL, no root access required)

### Install Chrome on Ubuntu (non-root)

```bash
# Download Chrome deb package
wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb

# Install (requires sudo for dpkg)
sudo dpkg -i google-chrome-stable_current_amd64.deb
sudo apt-get install -f

# Verify
google-chrome --version
```

chromedp will automatically find and use the installed Chrome binary.

---

## Quick Start

### 1. Clone into your new repo

```bash
git clone <your-repo-url>
cd airbnb-scrapper
```

### 2. Set up environment

```bash
cp .env.example .env
```

The defaults work out of the box. To change the target city:

```env
AIRBNB_SEARCH_LOCATION=Bangkok
```

To watch the browser while scraping (useful for debugging):

```env
HEADLESS=false
```

### 3. Start PostgreSQL

```bash
docker compose up -d
docker compose ps   # wait for status: healthy
```

### 4. Install Go dependencies

```bash
go mod tidy
```

### 5. Run

```bash
go run main.go
```

---

## What Gets Created at Runtime

| Item | Created by | Location |
|---|---|---|
| output/ folder | os.MkdirAll in csv.go | Project root |
| raw_listings.csv | os.Create in csv.go | output/raw_listings.csv |
| listings table | migrate() in postgres.go | PostgreSQL |
| DB indexes | migrate() in postgres.go | PostgreSQL |

Nothing needs to be created manually.

---

## Configuration Reference

| Variable | Default | Description |
|---|---|---|
| DB_HOST | localhost | PostgreSQL host |
| DB_PORT | 5432 | PostgreSQL port |
| DB_USER | scraper | Database user |
| DB_PASSWORD | scraperpass | Database password |
| DB_NAME | airbnb_scraper | Database name |
| LISTINGS_PER_PAGE | 5 | Number of listings to collect per page |
| PAGES_TO_SCRAPE | 2 | Number of search result pages to scrape |
| PAGE_LOAD_TIMEOUT_SEC | 60 | Timeout per page navigation in seconds |
| ACTION_TIMEOUT_SEC | 30 | Timeout for individual DOM actions in seconds |
| RATE_LIMIT_MS | 4000 | Minimum delay between requests in milliseconds |
| RANDOM_DELAY_MS | 3000 | Maximum random jitter on top of base delay |
| MAX_RETRIES | 3 | Retry attempts per failed navigation |
| HEADLESS | true | Run Chrome headlessly (set false to watch browser) |
| AIRBNB_SEARCH_LOCATION | Kuala Lumpur | City to search on Airbnb |
| OUTPUT_CSV | ./output/raw_listings.csv | Path for raw CSV output |

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
    amenities   TEXT          NOT NULL DEFAULT '',
    scraped_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);
```

### Useful queries

```sql
-- View all listings
SELECT title, price, location, rating FROM listings ORDER BY price DESC;

-- Average price per location
SELECT location, AVG(price)::numeric(10,2) AS avg_price, COUNT(*) AS total
FROM listings GROUP BY location ORDER BY avg_price DESC;

-- Top rated
SELECT title, rating, location FROM listings
WHERE rating > 0 ORDER BY rating DESC LIMIT 5;

-- Check amenities
SELECT title, amenities FROM listings WHERE amenities != '';
```

---

## Debugging Tips

**Set HEADLESS=false** to open a real browser window and watch what chromedp does. This is the most useful debugging tool.

**Airbnb changed its selectors** — this happens regularly. Open browser DevTools on the search page, inspect the listing card elements, and update the JS selector strings in scraper/airbnb/scraper.go.

**CAPTCHA or challenge page** — increase RATE_LIMIT_MS and RANDOM_DELAY_MS to slow down requests. If the problem persists, Airbnb may have flagged your IP temporarily. Wait 30-60 minutes before retrying.

**Zero listings scraped** — run with HEADLESS=false to see exactly what page chromedp is landing on.

---

## Disclaimer

Always comply with a website's robots.txt and Terms of Service. Use respectful request rates and do not scrape at scale.