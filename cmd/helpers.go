package cmd

import (
	"fmt"
	"time"
)

// ts renders a timestamp in a compact, human, local-time form for tables.
func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// tsp renders an optional timestamp, empty when nil.
func tsp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

// strv dereferences an optional string field, empty when nil.
func strv(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// enumv dereferences an optional string-enum field to its string value.
func enumv[T ~string](e *T) string {
	if e == nil {
		return ""
	}
	return string(*e)
}

// humanBytes renders a byte count the way a storage quota should read. The
// library's limits are stated in MiB and GiB, so the units are binary.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
