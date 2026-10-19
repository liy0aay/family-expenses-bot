package domain

import (
	"errors"
	"testing"
)

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"housing with е", "жилье", CategoryHousing, nil},
		{"housing with ё", "Жильё", CategoryHousing, nil},
		{"spaces trimmed", "  еда ", CategoryFood, nil},
		{"upper case", "ТРАНСПОРТ", CategoryTransport, nil},
		{"slug as is", "food", CategoryFood, nil},
		{"slug upper case", "FOOD", CategoryFood, nil},
		{"other category", "Другое", CategoryOther, nil},
		{"unknown word rejected", "кафе", "", ErrInvalidCategory},
		{"dictionary word is not a category", "ёлка", "", ErrInvalidCategory},
		{"empty string rejected", "   ", "", ErrInvalidCategory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCategory(tt.raw)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NormalizeCategory(%q) error = %v, want %v", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeCategory(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeCategory(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCategoryReference(t *testing.T) {
	if len(Categories) != 7 {
		t.Fatalf("got %d categories, want 7", len(Categories))
	}
	seen := make(map[string]bool, len(Categories))
	for _, slug := range Categories {
		if seen[slug] {
			t.Fatalf("duplicate slug %q", slug)
		}
		seen[slug] = true

		name, ok := CategoryRussianName(slug)
		if !ok || name == "" {
			t.Fatalf("slug %q has no Russian name", slug)
		}

		back, err := NormalizeCategory(name)
		if err != nil || back != slug {
			t.Fatalf("round trip %q -> %q (error %v), want %q", name, back, err, slug)
		}
	}

	if _, ok := CategoryRussianName("unknown"); ok {
		t.Fatal("unknown slug must not have a name")
	}
}
