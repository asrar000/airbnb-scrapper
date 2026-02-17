package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"airbnb-scraper/config"
	"airbnb-scraper/models"
	"airbnb-scraper/utils"

	_ "github.com/lib/pq"
)

// PostgresWriter implements Writer for PostgreSQL.
// It uses parameterized queries to prevent SQL injection and
// batch inserts for performance.
type PostgresWriter struct {
	db  *sql.DB
	log interface {
		Infof(string, ...interface{})
		Warnf(string, ...interface{})
		Errorf(string, ...interface{})
	}
}

// NewPostgresWriter opens a PostgreSQL connection and ensures the schema exists.
func NewPostgresWriter(cfg *config.Config) (*PostgresWriter, error) {
	log := utils.Logger()

	db, err := sql.Open("postgres", cfg.DB.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open DB connection: %w", err)
	}

	// Connection pool tuning
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connectivity
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	w := &PostgresWriter{db: db, log: log}
	if err := w.migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	log.Infof("PostgreSQL connected and schema ready")
	return w, nil
}

// migrate creates the listings table and indexes if they don't already exist.
func (w *PostgresWriter) migrate() error {
	ddl := `
	CREATE TABLE IF NOT EXISTS listings (
		id          BIGSERIAL PRIMARY KEY,
		platform    VARCHAR(20)  NOT NULL DEFAULT 'airbnb',
		title       TEXT         NOT NULL,
		price       NUMERIC(10,2) NOT NULL DEFAULT 0,
		location    VARCHAR(255) NOT NULL DEFAULT '',
		rating      NUMERIC(3,2) NOT NULL DEFAULT 0,
		url         TEXT         NOT NULL UNIQUE,
		description TEXT         NOT NULL DEFAULT '',
		scraped_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	);

	-- Indexes on high-cardinality fields used for filtering/reporting
	CREATE INDEX IF NOT EXISTS idx_listings_price    ON listings(price);
	CREATE INDEX IF NOT EXISTS idx_listings_location ON listings(location);
	CREATE INDEX IF NOT EXISTS idx_listings_platform ON listings(platform);
	CREATE INDEX IF NOT EXISTS idx_listings_rating   ON listings(rating);
	`

	_, err := w.db.Exec(ddl)
	return err
}

// Save batch-inserts listings into PostgreSQL using a single multi-row INSERT.
// It uses ON CONFLICT DO NOTHING to idempotently skip duplicates keyed by URL.
func (w *PostgresWriter) Save(listings []*models.Listing) error {
	if len(listings) == 0 {
		return nil
	}

	const batchSize = 50

	for i := 0; i < len(listings); i += batchSize {
		end := i + batchSize
		if end > len(listings) {
			end = len(listings)
		}
		batch := listings[i:end]

		if err := w.insertBatch(batch); err != nil {
			return fmt.Errorf("batch insert failed at offset %d: %w", i, err)
		}
		w.log.Infof("Inserted batch %d–%d (%d rows)", i+1, end, len(batch))
	}

	return nil
}

// insertBatch performs a single multi-row parameterized INSERT.
func (w *PostgresWriter) insertBatch(listings []*models.Listing) error {
	if len(listings) == 0 {
		return nil
	}

	// Build: INSERT INTO listings (...) VALUES ($1,$2,...),($7,$8,...),...
	// ON CONFLICT (url) DO NOTHING  — prevents duplicate URL entries
	const colCount = 8
	placeholders := make([]string, len(listings))
	args := make([]interface{}, 0, len(listings)*colCount)

	for i, l := range listings {
		base := i * colCount
		placeholders[i] = fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8,
		)
		args = append(args,
			string(l.Platform),
			l.Title,
			l.Price,
			l.Location,
			l.Rating,
			l.URL,
			l.Description,
			l.ScrapedAt,
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO listings (platform, title, price, location, rating, url, description, scraped_at)
		VALUES %s
		ON CONFLICT (url) DO NOTHING
	`, strings.Join(placeholders, ","))

	_, err := w.db.Exec(query, args...)
	return err
}

// Close closes the underlying database connection pool.
func (w *PostgresWriter) Close() error {
	return w.db.Close()
}

// DB exposes the raw *sql.DB for use by the service layer (read queries).
func (w *PostgresWriter) DB() *sql.DB {
	return w.db
}