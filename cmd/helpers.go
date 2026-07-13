package cmd

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
