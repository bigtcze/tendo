package postgres

import (
	"time"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

// timeOf returns midnight UTC of the calendar date; pgx encodes only the date part.
func timeOf(d schedule.Date) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}
