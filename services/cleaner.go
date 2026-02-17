package services

import (
	"strings"
	"time"

	"airbnb-scraper/models"
	"airbnb-scraper/scraper/airbnb"
	"airbnb-scraper/utils"
)

// Cleaner transforms raw scraped listings into normalized, deduplicated Listing records.
// It applies field normalization, price parsing, and URL-based deduplication.
type Cleaner struct {
	log interface {
		Infof(string, ...interface{})
		Warnf(string, ...interface{})
	}
}

// NewCleaner returns a configured Cleaner.
func NewCleaner() *Cleaner {
	return &Cleaner{log: utils.Logger()}
}

// Clean processes a slice of RawListings and returns clean Listings.
// Steps:
//  1. Parse and normalize price, rating, title, location
//  2. Filter out listings with no title or invalid price
//  3. Deduplicate by URL
func (c *Cleaner) Clean(raws []*models.RawListing) []*models.Listing {
	seen := make(map[string]bool, len(raws))
	results := make([]*models.Listing, 0, len(raws))

	for _, raw := range raws {
		// Skip duplicate URLs
		urlKey := strings.TrimSpace(strings.ToLower(raw.URL))
		if seen[urlKey] {
			c.log.Warnf("Duplicate URL skipped: %s", raw.URL)
			continue
		}
		seen[urlKey] = true

		listing := c.normalize(raw)
		if listing == nil {
			c.log.Warnf("Listing rejected (failed validation): %+v", raw)
			continue
		}

		results = append(results, listing)
	}

	c.log.Infof("Cleaned %d raw listings → %d valid listings", len(raws), len(results))
	return results
}

// normalize converts a single RawListing into a clean Listing.
// Returns nil if the listing is invalid (e.g., missing title).
func (c *Cleaner) normalize(raw *models.RawListing) *models.Listing {
	title := airbnb.NormalizeTitle(raw.Title)
	if title == "" {
		return nil
	}

	price := airbnb.ParsePrice(raw.RawPrice)
	// Accept price of 0 for listings that don't display price (contact host, etc.)
	// but log a warning so we can investigate
	if price == 0 {
		c.log.Warnf("Zero price for listing: %s (%s)", title, raw.URL)
	}

	scrapedAt := raw.ScrapedAt
	if scrapedAt.IsZero() {
		scrapedAt = time.Now()
	}

	return &models.Listing{
		Platform:    raw.Platform,
		Title:       title,
		Price:       price,
		Location:    airbnb.NormalizeLocation(raw.Location),
		Rating:      airbnb.ParseRating(raw.Rating),
		URL:         strings.TrimSpace(raw.URL),
		Description: strings.TrimSpace(raw.Description),
		ScrapedAt:   scrapedAt,
	}
}