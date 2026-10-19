package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RublesToKopecks parses a ruble amount from a decimal string or json.Number.
func RublesToKopecks(raw any) (int64, error) {
	var s string
	switch v := raw.(type) {
	case string:
		s = v
	case json.Number:
		s = v.String()
	default:
		return 0, invalidAmount(fmt.Sprintf("unsupported type %T", raw))
	}
	return parseRubles(s)
}

func parseRubles(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, invalidAmount("empty amount")
	}

	rublesPart, kopecksPart, found := strings.Cut(s, ".")
	if found && strings.Contains(kopecksPart, ".") {
		return 0, invalidAmount("extra dot in amount")
	}
	if !allDigits(rublesPart) || (kopecksPart != "" && !allDigits(kopecksPart)) {
		return 0, invalidAmount("a number like 350 or 350.50 expected")
	}
	if len(kopecksPart) > 2 {
		return 0, invalidAmount("at most two decimal places")
	}

	rubles, err := strconv.ParseInt(rublesPart, 10, 64)
	if err != nil {
		return 0, invalidAmount("amount too large")
	}
	if rubles > math.MaxInt64/100 {
		return 0, invalidAmount("amount too large")
	}

	for len(kopecksPart) < 2 {
		kopecksPart += "0"
	}
	// at most two digits here, Atoi cannot fail
	kopecks, _ := strconv.Atoi(kopecksPart)

	sub := rubles * 100
	if int64(kopecks) > math.MaxInt64-sub {
		return 0, invalidAmount("amount too large")
	}
	total := sub + int64(kopecks)
	if total <= 0 {
		return 0, invalidAmount("amount must be positive")
	}
	return total, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func invalidAmount(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidAmount, reason)
}
