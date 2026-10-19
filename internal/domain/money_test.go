package domain

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestRublesToKopecks(t *testing.T) {
	tests := []struct {
		name    string
		raw     any
		want    int64
		wantErr error
	}{
		{"whole rubles", "350", 35000, nil},
		{"one decimal digit", "350.5", 35050, nil},
		{"trailing zero fraction", "123.0", 12300, nil},
		{"two decimal digits", "350.50", 35050, nil},
		{"leading zero in kopecks", "350.05", 35005, nil},
		{"minimum amount", "0.01", 1, nil},
		{"trimmed spaces", " 350 ", 35000, nil},
		{"json.Number from LLM tool call", json.Number("350.5"), 35050, nil},
		{"max int64 boundary", "92233720368547758.07", math.MaxInt64, nil},
		{"three decimal digits rejected", "350.505", 0, ErrInvalidAmount},
		{"zero rejected", "0", 0, ErrInvalidAmount},
		{"negative rejected", "-350", 0, ErrInvalidAmount},
		{"empty string rejected", "", 0, ErrInvalidAmount},
		{"non-numeric rejected", "abc", 0, ErrInvalidAmount},
		{"two dots rejected", "350.50.5", 0, ErrInvalidAmount},
		{"fraction without rubles rejected", ".5", 0, ErrInvalidAmount},
		{"above max int64 rejected", "92233720368547758.08", 0, ErrInvalidAmount},
		{"rubles overflow rejected", "99999999999999999999", 0, ErrInvalidAmount},
		{"unsupported type rejected", 350, 0, ErrInvalidAmount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RublesToKopecks(tt.raw)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("RublesToKopecks(%v) error = %v, want %v", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RublesToKopecks(%v) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("RublesToKopecks(%v) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}
