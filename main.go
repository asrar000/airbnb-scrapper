package main

import (
	"fmt"
	"os"

	"airbnb-scrapper/config"
	"airbnb-scrapper/scraper/airbnb"
	"airbnb-scrapper/services"
	"airbnb-scrapper/storage"
	"airbnb-scrapper/utils"
)

func main() {
	// ── Bootstrap ──────────────────────────────────────────────────────────────
	utils.InitLogger()
	defer utils.Sync()
	log := utils.Logger()

	log.Infof("=== Airbnb Scraper (chromedp) Starting ===")

	cfg, err := config.Load()
	if err != nil {
		log.Errorf("Config load failed: %v", err)
		os.Exit(1)
	}

	log.Infof("Target location : %s", cfg.Airbnb.SearchLocation)
	log.Infof("Pages to scrape : %d", cfg.Scraper.PagesToScrape)
	log.Infof("Listings/page   : %d", cfg.Scraper.ListingsPerPage)
	log.Infof("Headless mode   : %v", cfg.Scraper.Headless)

	// ── Storage setup ──────────────────────────────────────────────────────────

	// CSV receives RAW unprocessed listings — complete audit trail of scrape output
	csvWriter, err := storage.NewCSVWriter(cfg.Output.CSVPath)
	if err != nil {
		log.Errorf("CSV writer setup failed: %v", err)
		os.Exit(1)
	}

	// PostgreSQL receives only CLEAN, normalized listings — source of truth for insights
	pgWriter, err := storage.NewPostgresWriter(cfg)
	if err != nil {
		log.Warnf("PostgreSQL unavailable — clean data will not be persisted: %v", err)
	} else {
		defer pgWriter.Close()
	}

	// ── Step 1: Scrape ─────────────────────────────────────────────────────────
	scraper := airbnb.New(cfg)

	rawListings, err := scraper.Scrape()
	if err != nil {
		log.Errorf("Scraping failed: %v", err)
		os.Exit(1)
	}

	if len(rawListings) == 0 {
		fmt.Println()
		fmt.Println("WARNING: No listings were scraped.")
		fmt.Println("Possible causes:")
		fmt.Println("  - Airbnb changed its page structure (update JS selectors in scraper.go)")
		fmt.Println("  - The search returned no results for the configured location")
		fmt.Println("  - A CAPTCHA or bot-challenge page was served")
		fmt.Println("  - Set HEADLESS=false in .env to watch the browser and debug")
		fmt.Println()
	}

	// ── Step 2: Save RAW data to CSV ────────────────────────────────────────────
	log.Infof("Saving %d raw listings to CSV...", len(rawListings))
	if err := csvWriter.SaveRaw(rawListings); err != nil {
		log.Errorf("CSV write failed: %v", err)
	}

	// ── Step 3: Clean ──────────────────────────────────────────────────────────
	cleaner := services.NewCleaner()
	cleanListings := cleaner.Clean(rawListings)
	log.Infof("Clean listings: %d / %d", len(cleanListings), len(rawListings))

	// ── Step 4: Save CLEAN data to PostgreSQL ──────────────────────────────────
	if pgWriter != nil {
		if err := pgWriter.Save(cleanListings); err != nil {
			log.Errorf("PostgreSQL write failed: %v", err)
		} else {
			log.Infof("Clean listings saved to PostgreSQL (%d rows)", len(cleanListings))
		}
	}

	// ── Step 5: Generate insights from CLEAN data ──────────────────────────────
	insightSvc := services.NewInsightService()
	report := insightSvc.Generate(cleanListings)

	// ── Step 6: Print terminal report ─────────────────────────────────────────
	reporter := services.NewReporter()
	reporter.Print(report)

	log.Infof("=== Airbnb Scraper Complete ===")
}