package app

import (
	"fmt"
	"time"
	_ "time/tzdata" // The scratch image has no zoneinfo files.
)

// loadTimezone resolves TZ, the IANA zone whose calendar decides the current
// challenge year and tonight's movie. Unset means UTC, whatever the host's zone.
func loadTimezone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	location, err := time.LoadLocation(name)
	if err != nil || name == "Local" {
		return nil, fmt.Errorf("TZ must be an IANA time zone name, such as America/New_York")
	}
	return location, nil
}

// householdClock reports the current time in the household's time zone.
func householdClock(location *time.Location) func() time.Time {
	return func() time.Time { return time.Now().In(location) }
}
