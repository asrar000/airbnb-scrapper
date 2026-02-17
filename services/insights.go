package services

import (
	"math"
	"sort"

	"airbnb-scraper/models"
	"airbnb-scraper/utils"
)

// InsightService computes analytics from a clean dataset of listings.
// All computation is done in-memory on the provided slice.
type InsightService struct {
	log interface {
		Infof(string, ...interface{})
	}
}

// NewInsightService returns a configured InsightService.
func NewInsightService() *InsightService {
	return &InsightService{log: utils.Logger()}
}

// Generate computes all required insights from the provided clean listings.
func (s *InsightService) Generate(listings []*models.Listing) *models.InsightReport {
	report := &models.InsightReport{
		ListingsByLocation: make(map[string]int),
	}

	if len(listings) == 0 {
		s.log.Infof("No listings to analyze")
		return report
	}

	report.TotalListings = len(listings)

	var (
		totalPrice     float64
		priceCount     int
		minPrice       = math.MaxFloat64
		maxPrice       float64
		mostExpensive  *models.Listing
		withRating     []*models.Listing
	)

	for _, l := range listings {
		// Platform count
		if l.Platform == models.PlatformAirbnb {
			report.AirbnbListings++
		}

		// Price stats (exclude zero-price listings from averages)
		if l.Price > 0 {
			totalPrice += l.Price
			priceCount++

			if l.Price < minPrice {
				minPrice = l.Price
			}
			if l.Price > maxPrice {
				maxPrice = l.Price
				mostExpensive = l
			}
		}

		// Location bucketing
		loc := l.Location
		if loc == "" {
			loc = "Unknown"
		}
		report.ListingsByLocation[loc]++

		// Collect rated listings for top-5
		if l.Rating > 0 {
			withRating = append(withRating, l)
		}
	}

	// Average price
	if priceCount > 0 {
		report.AveragePrice = math.Round((totalPrice/float64(priceCount))*100) / 100
		report.MinPrice = minPrice
		report.MaxPrice = maxPrice
	}

	report.MostExpensive = mostExpensive

	// Top 5 highest-rated
	sort.Slice(withRating, func(i, j int) bool {
		return withRating[i].Rating > withRating[j].Rating
	})
	limit := 5
	if len(withRating) < limit {
		limit = len(withRating)
	}
	report.TopRatedListings = withRating[:limit]

	s.log.Infof("Insight generation complete: %d listings analyzed", len(listings))
	return report
}