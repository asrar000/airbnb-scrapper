package storage

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"airbnb-scrapper/models"
	"airbnb-scrapper/utils"
)

// CSVWriter implements RawWriter for CSV file output.
// It stores unprocessed, raw scraped data exactly as received from the scraper —
// no normalization, no filtering. This gives a full audit trail of what was scraped.
type CSVWriter struct {
	path string
	log  interface {
		Infof(string, ...interface{})
		Errorf(string, ...interface{})
	}
}

// NewCSVWriter returns a CSVWriter that will write to the given file path.
// The output directory is created automatically if it does not exist.
func NewCSVWriter(path string) (*CSVWriter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}
	return &CSVWriter{path: path, log: utils.Logger()}, nil
}

// SaveRaw writes all raw listings to the CSV file, overwriting any existing content.
// Fields are written as-is: prices are not parsed, ratings are not normalized.
// This preserves the original scraped strings for debugging and auditing purposes.
func (c *CSVWriter) SaveRaw(listings []*models.RawListing) error {
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header reflects raw field names to distinguish from the clean PostgreSQL schema
	header := []string{
		"platform",
		"title",
		"raw_price",  // raw string e.g. "$120 per night"
		"location",
		"rating",     // raw string e.g. "4.85"
		"url",
		"description",
		"scraped_at",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, l := range listings {
		row := []string{
			string(l.Platform),
			l.Title,
			l.RawPrice,   // unmodified raw price string
			l.Location,
			l.Rating,     // unmodified raw rating string
			l.URL,
			l.Description,
			l.ScrapedAt.Format(time.RFC3339),
		}
		if err := w.Write(row); err != nil {
			c.log.Errorf("Failed to write CSV row for %s: %v", l.URL, err)
		}
	}

	c.log.Infof("Raw CSV written to %s (%d rows)", c.path, len(listings))
	return w.Error()
}

// Close is a no-op for CSVWriter — the file handle is closed after each SaveRaw call.
func (c *CSVWriter) Close() error { return nil }