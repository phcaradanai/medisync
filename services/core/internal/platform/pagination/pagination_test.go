package pagination

import (
	"testing"
)

func TestNormalizePageSize(t *testing.T) {
	tests := []struct {
		name string
		size int32
		want int32
	}{
		{name: "default zero", size: 0, want: DefaultPageSize},
		{name: "default negative", size: -1, want: DefaultPageSize},
		{name: "custom", size: 20, want: 20},
		{name: "maximum", size: MaxPageSize, want: MaxPageSize},
		{name: "cap", size: 999, want: MaxPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizePageSize(tt.size); got != tt.want {
				t.Fatalf("NormalizePageSize(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}
