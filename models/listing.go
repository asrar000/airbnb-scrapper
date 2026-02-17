package models

import "time"

// Platform identifies which platform the listing came from.
type Platform string

const (
	PlatformAirbnb Platform = "airbnb"
)

// RawListing is the unprocessed data directly from chromedp.
// All fields are raw strings exactly as scraped — no normalization applied.
type RawListing struct {
	Title       string
	RawPrice    string // e.g. "RM 450 per night"
	Location    string
	Rating      string // e.g. "4.85" or "New" if no rating yet
	URL         string
	Description string // from detail page
	Amenities   string // from detail page, comma-separated
	Platform    Platform
	ScrapedAt   time.Time
}

// Listing is the clean, normalized record stored in PostgreSQL.
type Listing struct {
	ID          int64
	Platform    Platform
	Title       string
	Price       float64  // normalized numeric nightly price
	Location    string
	Rating      float64  // 0 if not available
	URL         string
	Description string
	Amenities   string
	ScrapedAt   time.Time
}

// InsightReport holds all computed analytics derived from clean listings.
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