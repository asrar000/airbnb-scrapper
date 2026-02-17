package airbnb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"airbnb-scrapper/config"
	"airbnb-scrapper/models"
	"airbnb-scrapper/utils"

	"github.com/chromedp/chromedp"
)

// Scraper drives a real Chromium browser via chromedp to scrape Airbnb.
// It handles navigation, pagination, detail page fetching, rate limiting,
// duplicate tracking, and retry logic — all implemented manually since
// chromedp provides only raw browser automation primitives.
type Scraper struct {
	cfg         *config.Config
	rateLimiter *utils.RateLimiter
	visited     map[string]bool
	mu          sync.Mutex
	log         *utils.ZapLogger
}

// ZapLogger alias so utils package logger is accessible by type name.
type ZapLogger = interface {
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Errorf(string, ...interface{})
}

// New creates and returns a configured Scraper.
func New(cfg *config.Config) *Scraper {
	return &Scraper{
		cfg:         cfg,
		rateLimiter: utils.NewRateLimiter(cfg.Scraper.RateLimit, cfg.Scraper.RandomDelay),
		visited:     make(map[string]bool),
		log:         utils.Logger(),
	}
}

// ── Browser setup ────────────────────────────────────────────────────────────

// newBrowserContext creates a chromedp context with stealth options.
// We manually set browser flags to reduce bot-detection fingerprinting
// since chromedp exposes no built-in anti-detection layer.
func (s *Scraper) newBrowserContext(parent context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", s.cfg.Scraper.Headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("window-size", "1920,1080"),
		chromedp.Flag("start-maximized", true),
		chromedp.UserAgent(randomUserAgent()),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("accept-lang", "en-US,en;q=0.9"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(func(format string, args ...interface{}) {
			s.log.Infof("[chromedp] "+format, args...)
		}),
	)

	cancel := func() {
		ctxCancel()
		allocCancel()
	}

	return ctx, cancel
}

// ── Main entry point ─────────────────────────────────────────────────────────

// Scrape navigates Airbnb, collects listings from the configured number of pages,
// and returns all raw listings. Detail pages are fetched for each card found.
func (s *Scraper) Scrape() ([]*models.RawListing, error) {
	s.log.Infof("Starting Airbnb chromedp scrape for: %s", s.cfg.Airbnb.SearchLocation)

	parentCtx, parentCancel := context.WithTimeout(
		context.Background(),
		time.Duration(s.cfg.Scraper.PagesToScrape+1)*s.cfg.Scraper.PageLoadTimeout,
	)
	defer parentCancel()

	ctx, cancel := s.newBrowserContext(parentCtx)
	defer cancel()

	// Inject stealth JS to hide automation signals
	if err := s.injectStealthScripts(ctx); err != nil {
		s.log.Warnf("Stealth injection failed (non-fatal): %v", err)
	}

	var allListings []*models.RawListing

	for page := 1; page <= s.cfg.Scraper.PagesToScrape; page++ {
		s.log.Infof("--- Scraping page %d ---", page)

		listings, err := s.scrapePage(ctx, page)
		if err != nil {
			s.log.Errorf("Page %d failed: %v", page, err)
			continue
		}

		s.log.Infof("Page %d: collected %d listings", page, len(listings))
		allListings = append(allListings, listings...)

		// Rate limit between pages
		if page < s.cfg.Scraper.PagesToScrape {
			s.log.Infof("Waiting before next page...")
			s.rateLimiter.Wait()
		}
	}

	s.log.Infof("Scrape complete. Total raw listings: %d", len(allListings))
	return allListings, nil
}

// ── Page scraping ─────────────────────────────────────────────────────────────

// scrapePage navigates to the correct page, extracts listing cards,
// then fetches details from each individual property page.
func (s *Scraper) scrapePage(ctx context.Context, pageNum int) ([]*models.RawListing, error) {
	// Build search URL
	searchURL := s.buildSearchURL(pageNum)
	s.log.Infof("Navigating to: %s", searchURL)

	// Navigate with retry
	var navErr error
	err := utils.WithRetry(s.cfg.Scraper.MaxRetries, 3*time.Second, func() error {
		navCtx, navCancel := context.WithTimeout(ctx, s.cfg.Scraper.PageLoadTimeout)
		defer navCancel()

		navErr = chromedp.Run(navCtx,
			chromedp.Navigate(searchURL),
			chromedp.WaitVisible(`body`, chromedp.ByQuery),
		)
		return navErr
	})
	if err != nil {
		return nil, fmt.Errorf("navigation failed after retries: %w", err)
	}

	// Let the page settle and JS render
	utils.RandomSleep(3000, 5000)

	// Dismiss any popups (cookie banners, translation dialogs)
	s.dismissPopups(ctx)

	// Scroll to trigger lazy-loaded cards
	s.scrollPage(ctx)

	// Extract card data from the listing grid
	cards, err := s.extractListingCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("card extraction failed: %w", err)
	}

	s.log.Infof("Found %d cards on page %d, taking first %d", len(cards), pageNum, s.cfg.Scraper.ListingsPerPage)

	// Take only the configured number of listings per page
	limit := s.cfg.Scraper.ListingsPerPage
	if len(cards) < limit {
		limit = len(cards)
	}
	cards = cards[:limit]

	// Fetch detail pages for each card
	var listings []*models.RawListing
	for i, card := range cards {
		s.log.Infof("Fetching details for listing %d/%d: %s", i+1, limit, card.URL)

		// Rate limit between detail page requests
		s.rateLimiter.Wait()

		detailed, err := s.fetchDetailPage(ctx, card)
		if err != nil {
			s.log.Warnf("Detail fetch failed for %s: %v — using card data only", card.URL, err)
			listings = append(listings, card)
			continue
		}

		listings = append(listings, detailed)
	}

	return listings, nil
}

// ── URL builder ───────────────────────────────────────────────────────────────

// buildSearchURL constructs the Airbnb search URL for the given page number.
// Airbnb uses an offset-based pagination via the `items_offset` query param.
func (s *Scraper) buildSearchURL(page int) string {
	location := strings.ReplaceAll(s.cfg.Airbnb.SearchLocation, " ", "+")
	offset := (page - 1) * 20 // Airbnb shows ~20 results per page

	if page == 1 {
		return fmt.Sprintf(
			"https://www.airbnb.com/s/%s/homes?tab_id=home_tab&type=&place_id=&refinement_paths%%5B%%5D=%%2Fhomes",
			location,
		)
	}

	return fmt.Sprintf(
		"https://www.airbnb.com/s/%s/homes?tab_id=home_tab&type=&refinement_paths%%5B%%5D=%%2Fhomes&items_offset=%d",
		location,
		offset,
	)
}

// ── Card extraction ───────────────────────────────────────────────────────────

// listingCard holds the raw data parsed from a search result card.
type listingCard struct {
	Title    string
	RawPrice string
	Location string
	Rating   string
	URL      string
}

// extractListingCards pulls listing data from the search results grid.
// Airbnb renders cards as itemprop="itemListElement" or data-testid anchors.
func (s *Scraper) extractListingCards(ctx context.Context) ([]*models.RawListing, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, s.cfg.Scraper.ActionTimeout)
	defer cancel()

	// Extract all listing URLs first via JS evaluation
	var urls []string
	err := chromedp.Run(timeoutCtx,
		chromedp.Evaluate(`
			(function() {
				var links = [];
				// Try multiple selectors Airbnb uses across different page versions
				var anchors = document.querySelectorAll('a[href*="/rooms/"]');
				anchors.forEach(function(a) {
					var href = a.href;
					if (href && href.includes('/rooms/') && !links.includes(href)) {
						links.push(href);
					}
				});
				return links;
			})()
		`, &urls),
	)
	if err != nil {
		return nil, fmt.Errorf("URL extraction JS failed: %w", err)
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("no listing URLs found on page — Airbnb may have changed its structure or blocked the request")
	}

	s.log.Infof("Found %d listing URLs via JS", len(urls))

	// Now extract card-level metadata for each listing
	var listings []*models.RawListing

	for _, rawURL := range urls {
		// Clean and deduplicate URL
		cleanURL := cleanListingURL(rawURL)

		s.mu.Lock()
		if s.visited[cleanURL] {
			s.mu.Unlock()
			continue
		}
		s.visited[cleanURL] = true
		s.mu.Unlock()

		// Extract card data for this specific URL via JS
		card := s.extractCardForURL(timeoutCtx, cleanURL)
		listings = append(listings, card)
	}

	return listings, nil
}

// extractCardForURL uses JS to find the card element associated with a URL
// and pull title, price, location, and rating from it.
func (s *Scraper) extractCardForURL(ctx context.Context, listingURL string) *models.RawListing {
	var title, price, location, rating string

	// Attempt to extract metadata from the card containing this URL
	script := fmt.Sprintf(`
		(function() {
			var result = {title: '', price: '', location: '', rating: ''};
			var anchors = document.querySelectorAll('a[href*="/rooms/"]');
			for (var i = 0; i < anchors.length; i++) {
				if (anchors[i].href === %q || anchors[i].href.startsWith(%q.split('?')[0])) {
					var card = anchors[i].closest('[data-testid="card-container"]') ||
					           anchors[i].closest('div[itemprop="itemListElement"]') ||
					           anchors[i].closest('div[class*="g1qv1ctd"]') ||
					           anchors[i].parentElement;

					if (card) {
						// Title
						var titleEl = card.querySelector('[data-testid="listing-card-title"]') ||
						              card.querySelector('div[class*="t1jojoys"]') ||
						              card.querySelector('div[class*="dir-ltr"]') ||
						              card.querySelector('span[class*="t1jojoys"]');
						if (titleEl) result.title = titleEl.innerText.trim();

						// Price
						var priceEl = card.querySelector('span[class*="_tyxjp1"]') ||
						              card.querySelector('div[class*="pquyp1l"]') ||
						              card.querySelector('span[data-testid="price-summary"]') ||
						              card.querySelector('span[class*="a8jt5op"]') ||
						              card.querySelector('div[class*="_1jo4hgw"]');
						if (priceEl) result.price = priceEl.innerText.trim();

						// Location / subtitle
						var locEl = card.querySelector('div[class*="t6mzqp7"]') ||
						            card.querySelector('span[class*="r4a59j5"]') ||
						            card.querySelector('div[data-testid="listing-card-subtitle"]');
						if (locEl) result.location = locEl.innerText.trim();

						// Rating
						var ratingEl = card.querySelector('span[class*="r1dxllyb"]') ||
						               card.querySelector('span[aria-label*="rating"]') ||
						               card.querySelector('div[class*="t5eq1io"]');
						if (ratingEl) result.rating = ratingEl.innerText.trim();
					}
					break;
				}
			}
			return result;
		})()
	`, listingURL, listingURL)

	var result map[string]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err == nil {
		title = result["title"]
		price = result["price"]
		location = result["location"]
		rating = result["rating"]
	}

	// Fallback: use location from config if not found on card
	if location == "" {
		location = s.cfg.Airbnb.SearchLocation
	}

	return &models.RawListing{
		Title:     title,
		RawPrice:  price,
		Location:  location,
		Rating:    rating,
		URL:       listingURL,
		Platform:  models.PlatformAirbnb,
		ScrapedAt: time.Now(),
	}
}

// ── Detail page fetching ──────────────────────────────────────────────────────

// fetchDetailPage navigates to an individual property page and enriches
// the listing with description and amenities not available on the search card.
func (s *Scraper) fetchDetailPage(ctx context.Context, base *models.RawListing) (*models.RawListing, error) {
	var description, amenities, title, price, location, rating string

	err := utils.WithRetry(s.cfg.Scraper.MaxRetries, 3*time.Second, func() error {
		detailCtx, cancel := context.WithTimeout(ctx, s.cfg.Scraper.PageLoadTimeout)
		defer cancel()

		if err := chromedp.Run(detailCtx,
			chromedp.Navigate(base.URL),
			chromedp.WaitVisible(`body`, chromedp.ByQuery),
		); err != nil {
			return fmt.Errorf("navigate to detail page: %w", err)
		}

		// Let JS render
		utils.RandomSleep(2500, 4000)

		// Extract all fields from the detail page
		extractScript := `
		(function() {
			var result = {
				title: '', price: '', location: '', rating: '',
				description: '', amenities: ''
			};

			// Title — multiple selectors for different page variants
			var titleSelectors = [
				'h1[data-testid="listing-overview-title"]',
				'h1.hghzvl1',
				'h1[class*="hghzvl"]',
				'section[data-testid="listing-page-header"] h1',
				'h1'
			];
			for (var i = 0; i < titleSelectors.length; i++) {
				var el = document.querySelector(titleSelectors[i]);
				if (el && el.innerText.trim()) {
					result.title = el.innerText.trim();
					break;
				}
			}

			// Price — look for nightly price display
			var priceSelectors = [
				'span[data-testid="price-summary"]',
				'div[class*="_1jo4hgw"]',
				'span[class*="_tyxjp1"]',
				'div[data-testid="book-it-default"] span[class*="a8jt5op"]',
				'div[class*="pquyp1l"] span',
				'span[class*="a8jt5op"]'
			];
			for (var i = 0; i < priceSelectors.length; i++) {
				var el = document.querySelector(priceSelectors[i]);
				if (el && el.innerText.trim()) {
					result.price = el.innerText.trim();
					break;
				}
			}

			// Location — breadcrumbs or subtitle
			var locSelectors = [
				'div[data-testid="listing-overview-subtitle"]',
				'nav[aria-label="Breadcrumb"] ol li:last-child span',
				'span[class*="r1dxllyb"]',
				'div[class*="s78n3tv"] span'
			];
			for (var i = 0; i < locSelectors.length; i++) {
				var el = document.querySelector(locSelectors[i]);
				if (el && el.innerText.trim()) {
					result.location = el.innerText.trim();
					break;
				}
			}

			// Rating
			var ratingSelectors = [
				'span[class*="r1dxllyb"]',
				'button[data-testid="pdp-reviews-highlight-banner-host-rating"] span',
				'span[aria-label*="rating"]',
				'div[class*="_17p6nbba"] span'
			];
			for (var i = 0; i < ratingSelectors.length; i++) {
				var el = document.querySelector(ratingSelectors[i]);
				if (el && el.innerText.trim()) {
					result.rating = el.innerText.trim();
					break;
				}
			}

			// Description — the "About this place" section
			var descSelectors = [
				'div[data-testid="listing-overview-description"]',
				'div[data-section-id="DESCRIPTION_DEFAULT"] span',
				'section[data-section-id="DESCRIPTION_DEFAULT"]',
				'div[class*="_1al1ofe"] span',
				'div[data-testid="description-text"] span'
			];
			for (var i = 0; i < descSelectors.length; i++) {
				var el = document.querySelector(descSelectors[i]);
				if (el && el.innerText.trim()) {
					result.description = el.innerText.trim().substring(0, 1000);
					break;
				}
			}

			// Amenities — collect all visible amenity items
			var amenityEls = document.querySelectorAll(
				'div[data-section-id="AMENITIES_DEFAULT"] div[class*="amenity"] span, ' +
				'div[class*="_aujnou"] div[class*="l7n4lsf"] span, ' +
				'div[data-testid="amenity-row"] div span'
			);
			var amenityList = [];
			amenityEls.forEach(function(el) {
				var text = el.innerText.trim();
				if (text && text.length > 1 && !amenityList.includes(text)) {
					amenityList.push(text);
				}
			});
			result.amenities = amenityList.slice(0, 20).join(', ');

			return result;
		})()
		`

		var detailResult map[string]string
		if err := chromedp.Run(detailCtx, chromedp.Evaluate(extractScript, &detailResult)); err != nil {
			return fmt.Errorf("detail page JS extraction: %w", err)
		}

		title = detailResult["title"]
		price = detailResult["price"]
		location = detailResult["location"]
		rating = detailResult["rating"]
		description = detailResult["description"]
		amenities = detailResult["amenities"]

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Merge: prefer detail page data, fall back to card data
	merged := &models.RawListing{
		Title:       firstNonEmpty(title, base.Title),
		RawPrice:    firstNonEmpty(price, base.RawPrice),
		Location:    firstNonEmpty(location, base.Location, s.cfg.Airbnb.SearchLocation),
		Rating:      firstNonEmpty(rating, base.Rating),
		URL:         base.URL,
		Description: description,
		Amenities:   amenities,
		Platform:    models.PlatformAirbnb,
		ScrapedAt:   time.Now(),
	}

	s.log.Infof("Detail fetched: %q | Price: %s | Rating: %s", merged.Title, merged.RawPrice, merged.Rating)
	return merged, nil
}

// ── Stealth and UX helpers ────────────────────────────────────────────────────

// injectStealthScripts runs JS that hides common automation fingerprints.
func (s *Scraper) injectStealthScripts(ctx context.Context) error {
	stealthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return chromedp.Run(stealthCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, exp, err := chromedp.Evaluate(`
				Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
				Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
				Object.defineProperty(navigator, 'plugins', {get: () => [1, 2, 3]});
			`, nil).Do(ctx)
			if exp != nil {
				return exp
			}
			return err
		}),
	)
}

// dismissPopups attempts to close common Airbnb modal dialogs.
func (s *Scraper) dismissPopups(ctx context.Context) {
	popupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try common close/dismiss buttons — errors are non-fatal
	_ = chromedp.Run(popupCtx,
		chromedp.Click(`button[aria-label="Close"]`, chromedp.ByQuery),
	)
	_ = chromedp.Run(popupCtx,
		chromedp.Click(`button[data-testid="modal-container"] button`, chromedp.ByQuery),
	)
}

// scrollPage scrolls down incrementally to trigger lazy-loading of listing cards.
func (s *Scraper) scrollPage(ctx context.Context) {
	scrollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for i := 0; i < 5; i++ {
		_ = chromedp.Run(scrollCtx, chromedp.Evaluate(
			`window.scrollBy(0, 600)`, nil,
		))
		time.Sleep(500 * time.Millisecond)
	}

	// Scroll back to top so cards are visible
	_ = chromedp.Run(scrollCtx, chromedp.Evaluate(`window.scrollTo(0, 0)`, nil))
	time.Sleep(500 * time.Millisecond)
}

// ── Utility helpers ───────────────────────────────────────────────────────────

// cleanListingURL strips query parameters and fragments from a listing URL
// so deduplication works correctly across different URL variants.
func cleanListingURL(raw string) string {
	if idx := strings.Index(raw, "?"); idx != -1 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, "#"); idx != -1 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// firstNonEmpty returns the first non-empty string from the provided candidates.
func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// randomUserAgent returns a realistic browser User-Agent string.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_2) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

func randomUserAgent() string {
	return userAgents[time.Now().UnixNano()%int64(len(userAgents))]
}