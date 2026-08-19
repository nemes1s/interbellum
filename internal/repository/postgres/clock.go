package postgres

import "time"

// nowUTC is the timestamp source for domain transitions computed in Go
// (completion time, in particular). Column defaults still use the database's
// now() for insert timestamps; this exists so that a value the domain computes
// and a value the database stamps do not disagree about time zone.
func nowUTC() time.Time { return time.Now().UTC() }
