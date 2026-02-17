package storage

import "airbnb-scrapper/models"

// RawWriter defines the contract for storing unprocessed scraped data.
// Used by the CSV backend to persist raw listings before cleaning.
type RawWriter interface {
	// SaveRaw persists a batch of raw listings as-is from the scraper.
	SaveRaw(listings []*models.RawListing) error

	// Close releases any underlying resources.
	Close() error
}

// Writer defines the contract for storing clean, normalized listings.
// Used by the PostgreSQL backend after data has been cleaned.
type Writer interface {
	// Save persists a batch of clean listings atomically.
	Save(listings []*models.Listing) error

	// Close releases any underlying resources.
	Close() error
}