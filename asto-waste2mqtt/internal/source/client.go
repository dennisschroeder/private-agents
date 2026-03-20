package source

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	ical "github.com/arran4/golang-ical"
)

// Config represents the connection to the ASTO ICS calendar
type Config struct {
	DistrictID string
}

// Client represents the connection to the ASTO data
type Client struct {
	config Config
	logger *slog.Logger
}

// NewClient creates a new ASTO source client
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	return &Client{
		config: cfg,
		logger: logger,
	}, nil
}

// Connect simulates connecting to a data source
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info("ASTO source client initialized", "district_id", c.config.DistrictID)
	return nil
}

func (c *Client) Disconnect() {
	c.logger.Info("ASTO source client shut down")
}

// FetchData retrieves and parses the ASTO ICS calendar
func (c *Client) FetchData(ctx context.Context) (map[string]string, error) {
	year := time.Now().Year()
	url := fmt.Sprintf("https://www.asto.de/abfallkalender/%d/abfallkalender-jahresdetail/jahrdetail-digital/staedte-listview/district-detailview/district-ical?tx_cctrashcalendar_fetrashcal[action]=iCalExport&tx_cctrashcalendar_fetrashcal[controller]=District&tx_cctrashcalendar_fetrashcal[district]=%s", year, c.config.DistrictID)

	c.logger.Debug("Fetching ICS calendar", "url", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ICS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	cal, err := ical.ParseCalendar(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ICS: %w", err)
	}

	now := time.Now().Truncate(24 * time.Hour)
	nextDates := make(map[string]time.Time)

	for _, event := range cal.Events() {
		start, err := event.GetStartAt()
		if err != nil {
			continue
		}

		if start.Before(now) {
			continue
		}

		summary := event.GetProperty(ical.ComponentPropertySummary).Value
		wasteType := c.normalizeWasteType(summary)

		if currentNext, exists := nextDates[wasteType]; !exists || start.Before(currentNext) {
			nextDates[wasteType] = start
		}
	}

	results := make(map[string]string)
	for k, v := range nextDates {
		results[k] = v.Format("2006-01-02")
	}

	return results, nil
}

func (c *Client) normalizeWasteType(summary string) string {
	s := strings.ToLower(summary)
	switch {
	case strings.Contains(s, "bio"):
		return "organic_waste"
	case strings.Contains(s, "rest"):
		return "residual_waste"
	case strings.Contains(s, "papier") || strings.Contains(s, "blau"):
		return "paper_waste"
	case strings.Contains(s, "gelb") || strings.Contains(s, "wertstoff"):
		return "recyclable_waste"
	default:
		return "other"
	}
}
