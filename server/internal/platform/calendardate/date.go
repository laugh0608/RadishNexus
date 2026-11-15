// Package calendardate validates optional, timezone-independent civil dates.
package calendardate

import "time"

func Valid(date *string) bool {
	if date == nil {
		return true
	}
	parsed, err := time.Parse(time.DateOnly, *date)
	return err == nil && parsed.Year() > 0 && parsed.Format(time.DateOnly) == *date
}

func Equal(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
