// Package pagination provides shared cursor pagination limits.
package pagination

const (
	DefaultPageSize int32 = 50
	MaxPageSize     int32 = 200
)

// NormalizePageSize applies the API default and maximum page size.
func NormalizePageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return MaxPageSize
	}
	return pageSize
}
