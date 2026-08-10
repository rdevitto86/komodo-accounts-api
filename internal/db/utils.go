package db

// Creates a partition key for an account.
func createPK(accountID string) string { return "ACCOUNT#" + accountID }

// Helper that dereferences a pointer, returning the zero value if nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// Helper that returns a pointer to a string if non-empty, otherwise nil.
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Helper that dereferences a slice pointer, returning nil if nil.
func derefSlice[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}

// Helper that returns a pointer to a slice if non-empty, otherwise nil.
func ptrSlice[T any](s []T) *[]T {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// Helper that returns a pointer to a map if non-empty, otherwise nil.
func optMap[K comparable, V any](m map[K]V) *map[K]V {
	if len(m) == 0 {
		return nil
	}
	return &m
}

// Helper that converts a slice of strings to a slice of a generic type.
func toAuthMethods[T ~string](in []string) []T {
	out := make([]T, len(in))
	for i, s := range in {
		out[i] = T(s)
	}
	return out
}
