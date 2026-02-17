package airbnb

import (
	"regexp"
	"strconv"
	"strings"
)

// priceRegex extracts numeric digits (and decimals) from a raw price string.
// Examples: "$120 per night" → "120", "€95.50/night" → "95.50"
var priceRegex = regexp.MustCompile(`[\d,]+\.?\d*`)

// ParsePrice converts a raw price string into a float64.
// Returns 0 if no numeric value can be extracted.
func ParsePrice(raw string) float64 {
	if raw == "" {
		return 0
	}

	// Remove thousands separators
	cleaned := strings.ReplaceAll(raw, ",", "")

	// Find first numeric match
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

// ParseRating converts a raw rating string into a float64.
// Returns 0 if unparseable.
func ParseRating(raw string) float64 {
	if raw == "" {
		return 0
	}

	// Strip any trailing text like " (123 reviews)"
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return 0
	}

	val, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	// Sanity check: Airbnb ratings are 0–5
	if val < 0 || val > 5 {
		return 0
	}
	return val
}

// NormalizeLocation cleans and normalizes a raw location string.
func NormalizeLocation(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "Unknown"
	}
	// Collapse multiple spaces/commas
	re := regexp.MustCompile(`\s{2,}`)
	cleaned = re.ReplaceAllString(cleaned, " ")
	return cleaned
}

// NormalizeTitle cleans a listing title string.
func NormalizeTitle(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	re := regexp.MustCompile(`\s{2,}`)
	return re.ReplaceAllString(cleaned, " ")
}