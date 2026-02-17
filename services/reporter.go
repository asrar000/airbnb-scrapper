package services

import (
	"fmt"
	"sort"
	"strings"

	"airbnb-scraper/models"
)

const (
	divider    = "═══════════════════════════════════════════════════════════"
	subDivider = "───────────────────────────────────────────────────────────"
)

// Reporter renders an InsightReport to stdout in a clean, readable format.
type Reporter struct{}

// NewReporter returns a Reporter instance.
func NewReporter() *Reporter { return &Reporter{} }

// Print outputs the full insight report to the terminal.
func (r *Reporter) Print(report *models.InsightReport) {
	fmt.Println()
	fmt.Println(divider)
	fmt.Println("  🏠  VACATION RENTAL MARKET INSIGHTS")
	fmt.Println(divider)

	// ── Overview ────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Printf("  %-30s %d\n", "Total Listings Scraped:", report.TotalListings)
	fmt.Printf("  %-30s %d\n", "Airbnb Listings:", report.AirbnbListings)
	fmt.Println()
	fmt.Println(subDivider)

	// ── Pricing ─────────────────────────────────────────────────────────────
	fmt.Println("  💰  PRICING SUMMARY")
	fmt.Println(subDivider)
	fmt.Printf("  %-30s $%.2f\n", "Average Price (per night):", report.AveragePrice)
	fmt.Printf("  %-30s $%.2f\n", "Minimum Price:", report.MinPrice)
	fmt.Printf("  %-30s $%.2f\n", "Maximum Price:", report.MaxPrice)
	fmt.Println()

	if report.MostExpensive != nil {
		fmt.Println("  🥇  MOST EXPENSIVE PROPERTY")
		fmt.Println(subDivider)
		fmt.Printf("  Title    : %s\n", truncate(report.MostExpensive.Title, 60))
		fmt.Printf("  Price    : $%.2f / night\n", report.MostExpensive.Price)
		fmt.Printf("  Location : %s\n", report.MostExpensive.Location)
		fmt.Printf("  URL      : %s\n", report.MostExpensive.URL)
		fmt.Println()
	}

	// ── Listings per Location ────────────────────────────────────────────────
	fmt.Println(subDivider)
	fmt.Println("  📍  LISTINGS PER LOCATION (Top 10)")
	fmt.Println(subDivider)

	// Sort locations by count descending
	type locCount struct {
		name  string
		count int
	}
	sorted := make([]locCount, 0, len(report.ListingsByLocation))
	for loc, cnt := range report.ListingsByLocation {
		sorted = append(sorted, locCount{loc, cnt})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for _, lc := range sorted[:limit] {
		bar := strings.Repeat("█", min(lc.count, 30))
		fmt.Printf("  %-30s %3d  %s\n", truncate(lc.name, 28)+":", lc.count, bar)
	}
	fmt.Println()

	// ── Top 5 Highest Rated ──────────────────────────────────────────────────
	if len(report.TopRatedListings) > 0 {
		fmt.Println(subDivider)
		fmt.Println("  ⭐  TOP 5 HIGHEST RATED PROPERTIES")
		fmt.Println(subDivider)
		for i, l := range report.TopRatedListings {
			stars := ratingStars(l.Rating)
			fmt.Printf("  %d. %-50s %.2f %s\n",
				i+1,
				truncate(l.Title, 48),
				l.Rating,
				stars,
			)
			fmt.Printf("     📍 %-48s\n", l.Location)
		}
		fmt.Println()
	}

	fmt.Println(divider)
	fmt.Println("  Report generated successfully.")
	fmt.Println(divider)
	fmt.Println()
}

// truncate shortens s to maxLen characters, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// ratingStars converts a float rating to a visual star string.
func ratingStars(r float64) string {
	full := int(r)
	stars := strings.Repeat("★", full)
	if full < 5 {
		stars += strings.Repeat("☆", 5-full)
	}
	return stars
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}