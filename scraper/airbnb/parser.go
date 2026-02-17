package airbnb

import (
	"regexp"
	"strconv"
	"strings"
)

// priceRegex matches the first numeric value (with optional decimal) in a string.
// Examples: "RM 450 per night" -> "450", "$1,200/night" -> "1200"
var priceRegex = regexp.MustCompile(`[\d,]+\.?\d*`)

// ParsePrice extracts a float64 nightly price from a raw price string.
// Returns 0 if no numeric value can be found.
func ParsePrice(raw string) float64 {
	if raw == "" {
		return 0
	}

	// Remove thousands separators before matching
	cleaned := strings.ReplaceAll(raw, ",", "")
	match := priceRegex.FindString(cleaned)
	if match == "" {
		return 0
	}

	val, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	return val
}

// ParseRating converts a raw rating string to a float64.
// Handles formats like "4.85", "4.85 (123)", "New".
// Returns 0 if the value is not a valid numeric rating.
func ParseRating(raw string) float64 {
	if raw == "" || strings.EqualFold(strings.TrimSpace(raw), "new") {
		return 0
	}

	// Take only the first token — handles "4.85 (123 reviews)"
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return 0
	}

	val, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	// Sanity check: Airbnb ratings are between 0 and 5
	if val < 0 || val > 5 {
		return 0
	}
	return val
}

// NormalizeLocation cleans a raw location string.
func NormalizeLocation(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "Unknown"
	}
	re := regexp.MustCompile(`\s{2,}`)
	return re.ReplaceAllString(cleaned, " ")
}

// NormalizeTitle cleans a listing title.
func NormalizeTitle(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	re := regexp.MustCompile(`\s{2,}`)
	return re.ReplaceAllString(cleaned, " ")
}