package models

import (
	"time"
)

type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Price       string    `json:"price"`
	Currency    string    `json:"currency"`
	URL         string    `json:"url"`
	Image       string    `json:"image"`
	Rating      string    `json:"rating,omitempty"`
	Reviews     string    `json:"reviews,omitempty"`
	Source      string    `json:"source"`
	ScrapedAt   time.Time `json:"scraped_at"`
	InStock     bool      `json:"in_stock"`
	Description string    `json:"description,omitempty"`
	PriceValue  float64   `json:"price_value,omitempty"` // For filtering/sorting
}

type SearchResponse struct {
	Query      string    `json:"query"`
	Products   []Product `json:"products"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	TotalPages int       `json:"total_pages"`
	Source     string    `json:"source"`
	Filters    *Filters  `json:"filters,omitempty"`
	Sort       *Sort     `json:"sort,omitempty"`
	Duration   string    `json:"duration"`
}

type Filters struct {
	MinPrice  float64 `json:"min_price,omitempty"`
	MaxPrice  float64 `json:"max_price,omitempty"`
	InStock   *bool   `json:"in_stock,omitempty"`
	MinRating float64 `json:"min_rating,omitempty"`
	Source    string  `json:"source,omitempty"`
}

type Sort struct {
	Field string `json:"field"` // price, rating, name
	Order string `json:"order"` // asc, desc
}

type SearchParams struct {
	Query   string   `json:"query"`
	Country string   `json:"country"`
	Page    int      `json:"page"`
	Limit   int      `json:"limit"`
	Filters *Filters `json:"filters,omitempty"`
	Sort    *Sort    `json:"sort,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
