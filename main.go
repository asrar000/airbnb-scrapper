package main

import (
	"fmt"
	"os"

	"airbnb-scraper/config"
	"airbnb-scraper/scraper/airbnb"
	"airbnb-scraper/services"
	"airbnb-scraper/storage"
	"airbnb-scraper/utils"
)

func main() {
	// ── Bootstrap ─────────────────────────────────────────────────────────────
	utils.InitLogger()
	defer utils.Sync()
	log := utils.Logger()

	log.Infof("=== Airbnb Scraper Starting ===")

	// Load configuration from .env / environment
	cfg, err := config.Load()
	if err != nil {
		log.Errorf("Config load failed: %v", err)
		os.Exit(1)
	}

	// ── Storage setup ─────────────────────────────────────────────────────────

	// CSV receives RAW unprocessed listings straight from the scraper.
	// This acts as a full audit trail of everything that was scraped.
	csvWriter, err := storage.NewCSVWriter(cfg.OutputCSV)
	if err != nil {
		log.Errorf("CSV writer setup failed: %v", err)
		os.Exit(1)
	}

	// PostgreSQL receives only CLEAN, normalized listings after processing.
	// Insights are generated from this clean dataset.
	pgWriter, err := storage.NewPostgresWriter(cfg)
	if err != nil {
		log.Warnf("PostgreSQL unavailable — clean data will not be persisted: %v", err)
	} else {
		defer pgWriter.Close()
	}

	// ── Scraping ──────────────────────────────────────────────────────────────
	scraper, err := airbnb.New(cfg)
	if err != nil {
		log.Errorf("Failed to initialize scraper: %v", err)
		os.Exit(1)
	}

	rawListings, err := scraper.Scrape()
	if err != nil {
		log.Errorf("Scraping failed: %v", err)
		os.Exit(1)
	}

	if len(rawListings) == 0 {
		fmt.Println()
		fmt.Println("  No listings were scraped.")
		fmt.Println("   This is likely because Airbnb's anti-bot protection is active.")
		fmt.Println("   See the README for strategies to improve scrape success rate.")
		fmt.Println()
	}

	// ── Step 1: Persist RAW data → CSV ────────────────────────────────────────
	// Saved before any cleaning so we have a complete audit log of the scrape,
	// including listings that may later be filtered out as invalid.
	log.Infof("Saving %d raw listings to CSV...", len(rawListings))
	if err := csvWriter.SaveRaw(rawListings); err != nil {
		log.Errorf("CSV raw write failed: %v", err)
	}

	// ── Step 2: Clean the raw data ────────────────────────────────────────────
	// Normalizes prices, ratings, locations; removes duplicates and invalid entries.
	cleaner := services.NewCleaner()
	cleanListings := cleaner.Clean(rawListings)
	log.Infof("Cleaned listings ready: %d / %d", len(cleanListings), len(rawListings))

	// ── Step 3: Persist CLEAN data → PostgreSQL ───────────────────────────────
	// Only the validated, normalized dataset goes into the database.
	// Insights are derived from this same clean dataset.
	if pgWriter != nil {
		if err := pgWriter.Save(cleanListings); err != nil {
			log.Errorf("PostgreSQL write failed: %v", err)
		} else {
			log.Infof("Clean listings saved to PostgreSQL (%d rows)", len(cleanListings))
		}
	}

	// ── Step 4: Generate insights from CLEAN data ─────────────────────────────
	insightSvc := services.NewInsightService()
	report := insightSvc.Generate(cleanListings)

	// ── Step 5: Print terminal report ─────────────────────────────────────────
	reporter := services.NewReporter()
	reporter.Print(report)

	log.Infof("=== Airbnb Scraper Complete ===")
}