package airbnb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"airbnb-scrapper/config"
	"airbnb-scrapper/models"
	"airbnb-scrapper/utils"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

// Scraper drives a real Chromium browser via chromedp to scrape Airbnb.
type Scraper struct {
	cfg         *config.Config
	rateLimiter *utils.RateLimiter
	visited     map[string]bool
	mu          sync.Mutex
	log         *zap.SugaredLogger
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

func (s *Scraper) newBrowserContext(parent context.Context) (context.Context, context.CancelFunc) {
	_ = os.MkdirAll("./output", 0755)

	// Write chromedp/chrome stdout+stderr to a file (this works even if Chrome fails early)
	logFile, _ := os.OpenFile("./output/chromedp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	// Start from defaults (known compatible), then override exec path + flags.
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)

	// Force your Chrome binary
	opts = append(opts, chromedp.ExecPath("/usr/bin/google-chrome"))

	// Keep flags conservative/compatible
	opts = append(opts,
		chromedp.Flag("headless", s.cfg.Scraper.Headless), // bool, compatible
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("window-size", "1280,720"),
		chromedp.Flag("lang", "en-US"),
		chromedp.UserAgent(utils.RandomUserAgent()),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)

	ctx, ctxCancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithLogf(func(format string, args ...interface{}) {
			s.log.Infof("[chromedp] "+format, args...)
		}),
		chromedp.WithErrorf(func(format string, args ...interface{}) {
			s.log.Errorf("[chromedp] "+format, args...)
		}),
		chromedp.WithBrowserOption(chromedp.WithCombinedOutput(logFile)),
	)

	cancel := func() {
		ctxCancel()
		allocCancel()
		_ = logFile.Close()
	}
	return ctx, cancel
}

func (s *Scraper) bootBrowser(ctx context.Context) error {
	bootCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var title string
	err := chromedp.Run(bootCtx,
		chromedp.Navigate("data:text/html,<html><head><title>boot</title></head><body>boot</body></html>"),
		chromedp.Title(&title),
	)
	if err != nil {
		return fmt.Errorf("chrome boot failed: %w", err)
	}
	if title != "boot" {
		return fmt.Errorf("chrome boot failed: unexpected title %q", title)
	}
	return nil
}

// ── Main entry point ─────────────────────────────────────────────────────────

func (s *Scraper) Scrape() ([]*models.RawListing, error) {
	s.log.Infof("Starting Airbnb chromedp scrape for: %s", s.cfg.Airbnb.SearchLocation)

	parentCtx, parentCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer parentCancel()

	var allListings []*models.RawListing

	for page := 1; page <= s.cfg.Scraper.PagesToScrape; page++ {
		s.log.Infof("--- Scraping page %d ---", page)

		ctx, cancel := s.newBrowserContext(parentCtx)
		func() {
			defer cancel()

			if err := s.bootBrowser(ctx); err != nil {
				s.log.Errorf("Page %d failed early: %v", page, err)
				s.log.Errorf("Check ./output/chromedp.log for Chrome/chromedp stderr")
				return
			}

			listings, err := s.scrapePage(ctx, page)
			if err != nil {
				s.log.Errorf("Page %d failed: %v", page, err)
				s.log.Errorf("Check ./output/chromedp.log for Chrome/chromedp stderr")
				return
			}

			s.log.Infof("Page %d: collected %d listings", page, len(listings))
			allListings = append(allListings, listings...)
		}()

		if page < s.cfg.Scraper.PagesToScrape {
			s.log.Infof("Waiting before next page...")
			s.rateLimiter.Wait()
		}
	}

	s.log.Infof("Scrape complete. Total raw listings: %d", len(allListings))
	return allListings, nil
}

// ── Page scraping ─────────────────────────────────────────────────────────────

func (s *Scraper) scrapePage(ctx context.Context, pageNum int) ([]*models.RawListing, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("chromedp ctx already canceled before navigation: %w", err)
	}

	searchURL := s.buildSearchURL(pageNum)
	s.log.Infof("Navigating to: %s", searchURL)

	err := utils.WithRetry(s.cfg.Scraper.MaxRetries, 3*time.Second, func() error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("chromedp ctx canceled (browser likely dead): %w", err)
		}

		navCtx, navCancel := context.WithTimeout(ctx, s.cfg.Scraper.PageLoadTimeout)
		defer navCancel()

		return chromedp.Run(navCtx,
			chromedp.Navigate(searchURL),
			chromedp.WaitVisible(`body`, chromedp.ByQuery),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("navigation failed after retries: %w", err)
	}

	utils.RandomSleep(3000, 5000)

	s.dismissPopups(ctx)
	s.scrollPage(ctx)

	cards, err := s.extractListingCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("card extraction failed: %w", err)
	}

	s.log.Infof("Found %d cards on page %d, taking first %d", len(cards), pageNum, s.cfg.Scraper.ListingsPerPage)

	limit := s.cfg.Scraper.ListingsPerPage
	if len(cards) < limit {
		limit = len(cards)
	}
	cards = cards[:limit]

	var listings []*models.RawListing
	for i, card := range cards {
		s.log.Infof("Fetching details for listing %d/%d: %s", i+1, limit, card.URL)

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

func (s *Scraper) buildSearchURL(page int) string {
	location := strings.ReplaceAll(s.cfg.Airbnb.SearchLocation, " ", "+")
	offset := (page - 1) * 20

	if page == 1 {
		return fmt.Sprintf(
			"https://www.airbnb.com/s/%s/homes?tab_id=home_tab&refinement_paths%%5B%%5D=%%2Fhomes",
			location,
		)
	}

	return fmt.Sprintf(
		"https://www.airbnb.com/s/%s/homes?tab_id=home_tab&refinement_paths%%5B%%5D=%%2Fhomes&items_offset=%d",
		location,
		offset,
	)
}

// ── Card extraction ───────────────────────────────────────────────────────────

func (s *Scraper) extractListingCards(ctx context.Context) ([]*models.RawListing, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, s.cfg.Scraper.ActionTimeout)
	defer cancel()

	var urls []string
	err := chromedp.Run(timeoutCtx,
		chromedp.Evaluate(`
			(function() {
				var links = [];
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
		return nil, fmt.Errorf("no listing URLs found — Airbnb may have blocked the request or changed its structure")
	}

	s.log.Infof("Found %d listing URLs via JS", len(urls))

	var listings []*models.RawListing
	for _, rawURL := range urls {
		cleanURL := cleanListingURL(rawURL)

		s.mu.Lock()
		if s.visited[cleanURL] {
			s.mu.Unlock()
			continue
		}
		s.visited[cleanURL] = true
		s.mu.Unlock()

		card := s.extractCardForURL(timeoutCtx, cleanURL)
		listings = append(listings, card)
	}

	return listings, nil
}

func (s *Scraper) extractCardForURL(ctx context.Context, listingURL string) *models.RawListing {
	var title, price, location, rating string

	script := fmt.Sprintf(`
		(function() {
			var result = {title: '', price: '', location: '', rating: ''};
			var anchors = document.querySelectorAll('a[href*="/rooms/"]');
			for (var i = 0; i < anchors.length; i++) {
				if (anchors[i].href === %q || anchors[i].href.startsWith(%q.split('?')[0])) {
					var card = anchors[i].closest('[data-testid="card-container"]') ||
					           anchors[i].closest('div[itemprop="itemListElement"]') ||
					           anchors[i].parentElement;

					if (card) {
						var titleEl = card.querySelector('[data-testid="listing-card-title"]') ||
						              card.querySelector('div[class*="t1jojoys"]') ||
						              card.querySelector('span[class*="t1jojoys"]');
						if (titleEl) result.title = titleEl.innerText.trim();

						var priceEl = card.querySelector('span[class*="_tyxjp1"]') ||
						              card.querySelector('div[class*="pquyp1l"]') ||
						              card.querySelector('span[class*="a8jt5op"]');
						if (priceEl) result.price = priceEl.innerText.trim();

						var locEl = card.querySelector('div[class*="t6mzqp7"]') ||
						            card.querySelector('span[class*="r4a59j5"]') ||
						            card.querySelector('div[data-testid="listing-card-subtitle"]');
						if (locEl) result.location = locEl.innerText.trim();

						var ratingEl = card.querySelector('span[class*="r1dxllyb"]') ||
						               card.querySelector('span[aria-label*="rating"]');
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

func (s *Scraper) fetchDetailPage(ctx context.Context, base *models.RawListing) (*models.RawListing, error) {
	var description, amenities, title, price, location, rating string

	err := utils.WithRetry(s.cfg.Scraper.MaxRetries, 3*time.Second, func() error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("chromedp ctx canceled (browser likely dead): %w", err)
		}

		detailCtx, cancel := context.WithTimeout(ctx, s.cfg.Scraper.PageLoadTimeout)
		defer cancel()

		if err := chromedp.Run(detailCtx,
			chromedp.Navigate(base.URL),
			chromedp.WaitVisible(`body`, chromedp.ByQuery),
		); err != nil {
			return fmt.Errorf("navigate to detail page: %w", err)
		}

		utils.RandomSleep(2500, 4000)

		extractScript := `
		(function() {
			var result = {
				title: '', price: '', location: '', rating: '',
				description: '', amenities: ''
			};

			var titleSelectors = [
				'h1[data-testid="listing-overview-title"]',
				'h1.hghzvl1',
				'h1[class*="hghzvl"]',
				'section[data-testid="listing-page-header"] h1',
				'h1'
			];
			for (var i = 0; i < titleSelectors.length; i++) {
				var el = document.querySelector(titleSelectors[i]);
				if (el && el.innerText.trim()) { result.title = el.innerText.trim(); break; }
			}

			var priceSelectors = [
				'span[data-testid="price-summary"]',
				'div[class*="_1jo4hgw"]',
				'span[class*="_tyxjp1"]',
				'div[data-testid="book-it-default"] span[class*="a8jt5op"]',
				'span[class*="a8jt5op"]'
			];
			for (var i = 0; i < priceSelectors.length; i++) {
				var el = document.querySelector(priceSelectors[i]);
				if (el && el.innerText.trim()) { result.price = el.innerText.trim(); break; }
			}

			var locSelectors = [
				'div[data-testid="listing-overview-subtitle"]',
				'nav[aria-label="Breadcrumb"] ol li:last-child span',
				'div[class*="s78n3tv"] span'
			];
			for (var i = 0; i < locSelectors.length; i++) {
				var el = document.querySelector(locSelectors[i]);
				if (el && el.innerText.trim()) { result.location = el.innerText.trim(); break; }
			}

			var ratingSelectors = [
				'span[class*="r1dxllyb"]',
				'button[data-testid="pdp-reviews-highlight-banner-host-rating"] span',
				'span[aria-label*="rating"]',
				'div[class*="_17p6nbba"] span'
			];
			for (var i = 0; i < ratingSelectors.length; i++) {
				var el = document.querySelector(ratingSelectors[i]);
				if (el && el.innerText.trim()) { result.rating = el.innerText.trim(); break; }
			}

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

// ── UX helpers ────────────────────────────────────────────────────────────────

func (s *Scraper) dismissPopups(ctx context.Context) {
	popupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_ = chromedp.Run(popupCtx, chromedp.Click(`button[aria-label="Close"]`, chromedp.ByQuery))
	_ = chromedp.Run(popupCtx, chromedp.Click(`button[data-testid="modal-container"] button`, chromedp.ByQuery))
}

func (s *Scraper) scrollPage(ctx context.Context) {
	scrollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for i := 0; i < 5; i++ {
		_ = chromedp.Run(scrollCtx, chromedp.Evaluate(`window.scrollBy(0, 600)`, nil))
		time.Sleep(500 * time.Millisecond)
	}
	_ = chromedp.Run(scrollCtx, chromedp.Evaluate(`window.scrollTo(0, 0)`, nil))
	time.Sleep(500 * time.Millisecond)
}

// ── Utility helpers ───────────────────────────────────────────────────────────

func cleanListingURL(raw string) string {
	if idx := strings.Index(raw, "?"); idx != -1 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, "#"); idx != -1 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
