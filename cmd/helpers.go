package cmd

import "time"

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
