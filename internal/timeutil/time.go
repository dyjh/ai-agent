package timeutil

import "time"

// NowUTC returns the current UTC time.
func NowUTC() time.Time {
	return time.Now().UTC()
}
