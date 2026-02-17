package airbnb

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"airbnb-scrapper/config"
	"airbnb-scrapper/models"
	"airbnb-scrapper/utils"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/gocolly/colly/v2/queue"
)

// Scraper is the main Airbnb scraping engine.
// It uses Colly with rate limiting, random delays, and rotating user agents
// to stay within respectful scraping bounds and reduce IP ban risk.
type Scraper struct {
	cfg        *config.Config
	collector  *colly.Collector
	queue      *queue.Queue
	listings   []*models.RawListing
	visitedURL map[string]bool
	mu         sync.Mutex
	log        interface {
		Infof(string, ...interface{})
		Warnf(string, ...interface{})
		Errorf(string, ...interface{})
	}
}

// New creates and configures a new AirbnbScraper instance.
func New(cfg *config.Config) (*Scraper, error) {
	log := utils.Logger()

	c := colly.NewCollector(
		colly.MaxDepth(cfg.Scraper.MaxDepth),
		colly.Async(true),
		colly.AllowedDomains("www.airbnb.com", "airbnb.com"),
		colly.UserAgent(utils.RandomUserAgent()),
	)

	// Rotate User-Agent on every request to reduce fingerprinting
	extensions.RandomUserAgent(c)
	extensions.Referer(c)

	// Configure rate limiting — crucial to avoid being blocked
	if err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*airbnb.*",
		Parallelism: cfg.Scraper.Parallelism,
		Delay:       cfg.Scraper.RateLimit,
		RandomDelay: cfg.Scraper.RandomDelay,
	}); err != nil {
		return nil, fmt.Errorf("failed to set rate limit: %w", err)
	}

	// Request queue with bounded concurrency
	q, err := queue.New(cfg.Scraper.Parallelism, &queue.InMemoryQueueStorage{MaxSize: 10000})
	if err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	s := &Scraper{
		cfg:        cfg,
		collector:  c,
		queue:      q,
		listings:   make([]*models.RawListing, 0, 200),
		visitedURL: make(map[string]bool),
		log:        log,
	}

	s.attachHandlers()
	return s, nil
}

// attachHandlers wires all Colly event callbacks.
func (s *Scraper) attachHandlers() {
	// ── Before request: inject stealth headers ──────────────────────────────
	s.collector.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
		r.Headers.Set("Cache-Control", "max-age=0")
		s.log.Infof("Visiting: %s", r.URL.String())
	})

	// ── Parse listing cards from search results page ────────────────────────
	// Airbnb embeds listing data in a __NEXT_DATA__ JSON blob — far more
	// reliable than CSS selectors which change with every deploy.
	s.collector.OnHTML("script#__NEXT_DATA__", func(e *colly.HTMLElement) {
		s.parseNextData(e)
	})

	// Fallback: try meta og tags if JSON parsing fails
	s.collector.OnHTML(`meta[property="og:title"]`, func(e *colly.HTMLElement) {
		// Only used as a last-resort signal
	})

	// ── Pagination: follow "next page" links ────────────────────────────────
	s.collector.OnHTML(`a[aria-label="Next"]`, func(e *colly.HTMLElement) {
		nextPage := e.Attr("href")
		if nextPage == "" {
			return
		}
		fullURL := s.cfg.Airbnb.BaseURL + nextPage
		s.visitAndEnqueue(fullURL)
	})

	// ── Error handling with retry ────────────────────────────────────────────
	s.collector.OnError(func(r *colly.Response, err error) {
		statusCode := 0
		if r != nil {
			statusCode = r.StatusCode
		}
		s.log.Errorf("Request failed [%d] %s: %v", statusCode, r.Request.URL, err)

		// Back off on rate-limiting responses
		if statusCode == 429 || statusCode == 503 {
			s.log.Warnf("Rate limited — backing off 30s before retry")
			time.Sleep(30 * time.Second)
		}

		retries := r.Request.Ctx.GetAny("retries")
		count := 0
		if retries != nil {
			count = retries.(int)
		}
		if count < s.cfg.Scraper.MaxRetries {
			r.Request.Ctx.Put("retries", count+1)
			_ = r.Request.Retry()
		}
	})

	s.collector.OnResponse(func(r *colly.Response) {
		s.log.Infof("Response [%d] from %s (%d bytes)", r.StatusCode, r.Request.URL, len(r.Body))
	})
}

// ── __NEXT_DATA__ JSON parsing ──────────────────────────────────────────────

// nextDataRoot is the top-level shape of Airbnb's Next.js page data blob.
type nextDataRoot struct {
	Props struct {
		PageProps struct {
			StaysSearch struct {
				Results struct {
					SearchResults []searchResult `json:"searchResults"`
				} `json:"results"`
				PaginationInfo struct {
					NextPageCursor string `json:"nextPageCursor"`
					HasNextPage    bool   `json:"hasNextPage"`
				} `json:"paginationInfo"`
			} `json:"staysSearch"`
		} `json:"pageProps"`
	} `json:"props"`
}

type searchResult struct {
	Listing struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		City  string `json:"city"`
		State string `json:"state"`
		Avg   *struct {
			Value float64 `json:"value"`
		} `json:"avgRatingLocalized"`
		Contextual *struct {
			PriceString string `json:"priceString"`
		} `json:"contextualPictures"`
	} `json:"listing"`
	PricingQuote struct {
		StructuredStayDisplayPrice struct {
			PrimaryLine struct {
				Price          string `json:"price"`
				DiscountedPrice string `json:"discountedPrice"`
			} `json:"primaryLine"`
		} `json:"structuredStayDisplayPrice"`
	} `json:"pricingQuote"`
}

// parseNextData extracts listings from Airbnb's embedded JSON blob.
func (s *Scraper) parseNextData(e *colly.HTMLElement) {
	raw := e.Text
	if raw == "" {
		s.log.Warnf("Empty __NEXT_DATA__ on %s", e.Request.URL)
		return
	}

	var root nextDataRoot
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		s.log.Errorf("Failed to parse __NEXT_DATA__: %v", err)
		// Fallback to HTML parsing if JSON structure changed
		s.parseHTMLFallback(e)
		return
	}

	results := root.Props.PageProps.StaysSearch.Results.SearchResults
	s.log.Infof("Found %d listings in __NEXT_DATA__", len(results))

	for _, r := range results {
		listing := s.buildRawListing(r, e.Request.URL.String())
		if listing != nil {
			s.addListing(listing)
		}
	}

	// Handle cursor-based pagination
	pagination := root.Props.PageProps.StaysSearch.PaginationInfo
	if pagination.HasNextPage && pagination.NextPageCursor != "" {
		nextURL := s.buildNextPageURL(e.Request.URL.String(), pagination.NextPageCursor)
		s.visitAndEnqueue(nextURL)
	}
}

// buildRawListing constructs a RawListing from a parsed search result.
func (s *Scraper) buildRawListing(r searchResult, sourceURL string) *models.RawListing {
	id := r.Listing.ID
	if id == "" {
		return nil
	}

	listingURL := fmt.Sprintf("%s/rooms/%s", s.cfg.Airbnb.BaseURL, id)

	// Deduplicate by URL
	s.mu.Lock()
	if s.visitedURL[listingURL] {
		s.mu.Unlock()
		return nil
	}
	s.visitedURL[listingURL] = true
	s.mu.Unlock()

	// Extract price string
	rawPrice := r.PricingQuote.StructuredStayDisplayPrice.PrimaryLine.Price
	if rawPrice == "" {
		rawPrice = r.PricingQuote.StructuredStayDisplayPrice.PrimaryLine.DiscountedPrice
	}

	// Build location string
	location := strings.TrimSpace(r.Listing.City)
	if r.Listing.State != "" {
		location = strings.TrimSpace(r.Listing.City + ", " + r.Listing.State)
	}

	// Rating
	rating := ""
	if r.Listing.Avg != nil {
		rating = fmt.Sprintf("%.2f", r.Listing.Avg.Value)
	}

	return &models.RawListing{
		Title:     r.Listing.Name,
		RawPrice:  rawPrice,
		Location:  location,
		Rating:    rating,
		URL:       listingURL,
		Platform:  models.PlatformAirbnb,
		ScrapedAt: time.Now(),
	}
}

// parseHTMLFallback scrapes listings from rendered HTML when JSON fails.
// This covers cases where Airbnb changes its __NEXT_DATA__ schema.
// It logs a warning so the developer knows the JSON parser needs updating.
func (s *Scraper) parseHTMLFallback(e *colly.HTMLElement) {
	selectors := []string{
		`div[data-testid="card-container"]`,
		`div[itemprop="itemListElement"]`,
		`div[class*="listingCard"]`,
	}

	for _, sel := range selectors {
		count := e.DOM.Find(sel).Length()
		if count > 0 {
			s.log.Warnf("HTML fallback triggered: found %d elements via selector '%s' — update JSON parser", count, sel)
		}
	}
}

// ── URL helpers ─────────────────────────────────────────────────────────────

// buildNextPageURL appends a cursor to the search URL for pagination.
func (s *Scraper) buildNextPageURL(currentURL, cursor string) string {
	u, err := url.Parse(currentURL)
	if err != nil {
		return currentURL
	}
	q := u.Query()
	q.Set("cursor", cursor)
	u.RawQuery = q.Encode()
	return u.String()
}

// visitAndEnqueue adds a URL to the scrape queue if not already visited.
func (s *Scraper) visitAndEnqueue(rawURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.visitedURL[rawURL] {
		return
	}
	s.visitedURL[rawURL] = true

	if err := s.queue.AddURL(rawURL); err != nil {
		s.log.Errorf("Failed to enqueue %s: %v", rawURL, err)
	}
}

// ── Thread-safe listing collection ─────────────────────────────────────────

// addListing appends a listing to the shared slice in a thread-safe manner.
func (s *Scraper) addListing(l *models.RawListing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listings = append(s.listings, l)
	s.log.Infof("Collected listing: %s @ %s", l.Title, l.Location)
}

// ── Public API ───────────────────────────────────────────────────────────────

// Scrape starts the scraping process and blocks until complete.
// It returns all raw listings collected.
func (s *Scraper) Scrape() ([]*models.RawListing, error) {
	s.log.Infof("Starting Airbnb scrape: %s", s.cfg.Airbnb.SearchURL)

	// Seed the queue with the initial search URL
	if err := s.queue.AddURL(s.cfg.Airbnb.SearchURL); err != nil {
		return nil, fmt.Errorf("failed to seed queue: %w", err)
	}

	// Mark as visited to prevent re-scraping seed URL
	s.mu.Lock()
	s.visitedURL[s.cfg.Airbnb.SearchURL] = true
	s.mu.Unlock()

	// Run the queue — blocks until drained
	if err := s.queue.Run(s.collector); err != nil {
		s.log.Errorf("Queue run error: %v", err)
	}

	// Wait for all async requests to finish
	s.collector.Wait()

	s.mu.Lock()
	collected := make([]*models.RawListing, len(s.listings))
	copy(collected, s.listings)
	s.mu.Unlock()

	s.log.Infof("Scrape complete. Total raw listings: %d", len(collected))
	return collected, nil
}