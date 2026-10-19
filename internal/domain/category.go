package domain

import (
	"fmt"
	"strings"
)

const (
	CategoryFood          = "food"
	CategoryTransport     = "transport"
	CategoryHousing       = "housing"
	CategoryEntertainment = "entertainment"
	CategoryHealth        = "health"
	CategoryShopping      = "shopping"
	CategoryOther         = "other"
)

var Categories = []string{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryEntertainment,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

var categoryRussian = map[string]string{
	CategoryFood:          "еда",
	CategoryTransport:     "транспорт",
	CategoryHousing:       "жильё",
	CategoryEntertainment: "развлечения",
	CategoryHealth:        "здоровье",
	CategoryShopping:      "покупки",
	CategoryOther:         "другое",
}

var categoryAliases = map[string]string{
	"food":          CategoryFood,
	"еда":           CategoryFood,
	"transport":     CategoryTransport,
	"транспорт":     CategoryTransport,
	"housing":       CategoryHousing,
	"жилье":         CategoryHousing,
	"entertainment": CategoryEntertainment,
	"развлечения":   CategoryEntertainment,
	"health":        CategoryHealth,
	"здоровье":      CategoryHealth,
	"shopping":      CategoryShopping,
	"покупки":       CategoryShopping,
	"other":         CategoryOther,
	"другое":        CategoryOther,
}

// NormalizeCategory maps a slug or a Russian name (е/ё unified) to a slug.
func NormalizeCategory(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	key := strings.ReplaceAll(strings.ToLower(trimmed), "ё", "е")
	slug, ok := categoryAliases[key]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidCategory, trimmed)
	}
	return slug, nil
}

func CategoryRussianName(slug string) (string, bool) {
	name, ok := categoryRussian[slug]
	return name, ok
}
