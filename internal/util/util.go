// Package util holds small CLI helpers.
package util

// FirstNonEmpty returns the first non-empty string (flag > env > config precedence).
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// OrDash renders an empty string as a dash for table cells.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
