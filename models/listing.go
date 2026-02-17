package models

import "time"

// Platform represents which platform the listing was scraped from.
type Platform string

const (
	PlatformAirbnb Platform = "airbnb"
)

// RawListing is the unprocessed data directly from the scraper.
// Fields may contain raw strings that need normalization.
type RawListing struct {
	Title       string
	RawPrice    string // e.g. "$120 per night"
	Location    string
	Rating      string // e.g. "4.85"
	URL         string
	Description string
	Platform    Platform
	ScrapedAt   time.Time
}

// Listing is the clean, normalized DTO used throughout the system.
type Listing struct {
	ID          int64
	Platform    Platform
	Title       string
	Price       float64 // normalized numeric price per night
	Location    string
	Rating      float64 // 0 if not available
	URL         string
	Description string
	ScrapedAt   time.Time
}

// InsightReport is the aggregated analytics output printed to terminal.
type InsightReport struct {
	TotalListings      int
	AirbnbListings     int
	AveragePrice       float64
	MinPrice           float64
	MaxPrice           float64
	MostExpensive      *Listing
	TopRatedListings   []*Listing
	ListingsByLocation map[string]int
}