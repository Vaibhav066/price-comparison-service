package utils

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsePrice converts price string to float64
func ParsePrice(priceStr string) float64 {
	if priceStr == "" {
		return 0
	}

	// Remove currency symbols and clean up
	cleanPrice := strings.ReplaceAll(priceStr, "$", "")
	cleanPrice = strings.ReplaceAll(cleanPrice, ",", "")
	cleanPrice = strings.TrimSpace(cleanPrice)

	// Extract numeric value
	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(cleanPrice)
	if match == "" {
		return 0
	}

	price, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}

	return price
}

// ParseRating converts rating string to float64
func ParseRating(ratingStr string) float64 {
	if ratingStr == "" {
		return 0
	}

	// Extract numeric rating (e.g., "4.5 out of 5 stars" -> 4.5)
	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(ratingStr)
	if match == "" {
		return 0
	}

	rating, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}

	return rating
}
