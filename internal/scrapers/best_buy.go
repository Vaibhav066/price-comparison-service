package scrapers

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/debug"
	"price-comparison-api/internal/models"
)

type BestBuyScraper struct {
	collector *colly.Collector
}

func NewBestBuyScraper() *BestBuyScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("bestbuy.com", "www.bestbuy.com"),
		colly.Debugger(&debug.LogDebugger{}),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("DNT", "1")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*bestbuy.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Best Buy scraper error: %v", err)
	})

	return &BestBuyScraper{collector: c}
}

func (b *BestBuyScraper) Search(query, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	if strings.ToUpper(country) != "US" {
		log.Printf("Best Buy: Country %s not supported, returning empty results", country)
		return products, nil
	}

	searchURL := b.getSearchURL(query)
	log.Printf("Searching Best Buy (US) with URL: %s", searchURL)

	// Multiple selector strategies for Best Buy's product listings
	selectors := []string{
		".sku-item",
		"[data-testid='product-card']",
		".sr-item",
		".list-item",
		".product-item",
		"li.sku-item",
		"[data-sku-id]",
	}

	foundAny := false
	errorCount := 0

	b.collector.OnResponse(func(r *colly.Response) {
		log.Printf("Best Buy Response status: %d, Content-Length: %d", r.StatusCode, len(r.Body))
		bodyStr := string(r.Body)
		log.Printf("Page contains product data: %v", strings.Contains(bodyStr, "sku-item") || strings.Contains(bodyStr, "product"))
	})

	for _, selector := range selectors {
		log.Printf("Trying Best Buy selector: %s", selector)

		b.collector.OnHTML(selector, func(e *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    "Best Buy US",
				Currency:  "USD",
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Extract name with multiple fallback selectors
			nameSelectors := []string{
				".sku-header a",
				".sku-title",
				"h4.sr-product-title a",
				"h3.sr-product-title a",
				".sr-product-title",
				"a.v-fw-medium",
				".product-title",
				"[data-testid='product-title']",
				"h4 a",
			}

			for _, nameSelector := range nameSelectors {
				name := strings.TrimSpace(e.ChildText(nameSelector))
				if name == "" {
					// Try getting from title attribute
					name = strings.TrimSpace(e.ChildAttr(nameSelector, "title"))
				}

				if name != "" && len(name) > 5 && !b.isGenericTitle(name) {
					product.Name = b.cleanProductName(name)
					break
				}
			}

			if product.Name == "" {
				return // Skip if no valid name found
			}

			product.Price = b.extractPrice(e)
			product.URL = b.extractURL(e)
			product.Image = b.extractImage(e)
			product.Rating = b.extractRating(e)
			product.Reviews = b.extractReviews(e)

			if product.Price != "" {
				product.ID = fmt.Sprintf("bestbuy_us_%d", time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found Best Buy product: %s - %s", product.Name, product.Price)
			}
		})

		err := b.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting Best Buy with selector %s: %v", selector, err)
			errorCount++
			continue
		}

		// If we found products with this selector, break
		if foundAny {
			break
		}

		// Reset collector for next selector attempt
		b.collector = b.resetCollector()
		time.Sleep(2 * time.Second) // Additional delay between selector attempts
	}

	if !foundAny && errorCount == len(selectors) {
		log.Printf("Best Buy: No products found and all selectors failed for query: %s", query)
		return products, fmt.Errorf("all Best Buy scraping attempts failed")
	}

	if !foundAny {
		log.Printf("Best Buy: No products found for query: %s", query)
	}

	log.Printf("Best Buy found %d products", len(products))
	return products, nil
}

func (b *BestBuyScraper) resetCollector() *colly.Collector {
	c := colly.NewCollector(
		colly.AllowedDomains("bestbuy.com", "www.bestbuy.com"),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*bestbuy.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	return c
}

func (b *BestBuyScraper) getSearchURL(query string) string {
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	return fmt.Sprintf("https://www.bestbuy.com/site/searchpage.jsp?st=%s", encodedQuery)
}

func (b *BestBuyScraper) extractPrice(e *colly.HTMLElement) string {
	priceSelectors := []string{
		".sr-price .visuallyhidden",
		".pricing-price__range",
		".sku-price",
		".current-price",
		".sr-price",
		"[aria-label*='current price']",
		".price-current",
		"span.sr-price",
		".visually-hidden:contains('current price')",
		"span:contains('$')",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(e.ChildText(selector))
		if price != "" {
			formattedPrice := b.formatPrice(price)
			if formattedPrice != "" {
				return formattedPrice
			}
		}
	}

	// Try to extract price from aria-label
	priceFromLabel := e.ChildAttr("[aria-label*='current price']", "aria-label")
	if priceFromLabel != "" {
		return b.extractPriceFromText(priceFromLabel)
	}

	return ""
}

func (b *BestBuyScraper) extractURL(e *colly.HTMLElement) string {
	urlSelectors := []string{
		".sku-header a",
		"h4.sr-product-title a",
		"h3.sr-product-title a",
		"a.v-fw-medium",
		".product-title a",
		"a",
	}

	for _, selector := range urlSelectors {
		relativeURL := e.ChildAttr(selector, "href")
		if relativeURL != "" {
			if strings.HasPrefix(relativeURL, "http") {
				return relativeURL
			}
			if strings.HasPrefix(relativeURL, "/") {
				return "https://www.bestbuy.com" + relativeURL
			}
		}
	}

	return ""
}

func (b *BestBuyScraper) extractImage(e *colly.HTMLElement) string {
	imageSelectors := []string{
		"img.product-image",
		"img[src*='pisces.bbystatic.com']",
		"img[alt*='product']",
		"picture img",
		"img",
	}

	for _, selector := range imageSelectors {
		imgSrc := e.ChildAttr(selector, "src")
		if imgSrc != "" && (strings.Contains(imgSrc, "bestbuy") || strings.Contains(imgSrc, "bbystatic")) {
			return imgSrc
		}

		// Try data-src for lazy loading
		imgSrc = e.ChildAttr(selector, "data-src")
		if imgSrc != "" && (strings.Contains(imgSrc, "bestbuy") || strings.Contains(imgSrc, "bbystatic")) {
			return imgSrc
		}
	}

	return ""
}

func (b *BestBuyScraper) extractRating(e *colly.HTMLElement) string {
	ratingSelectors := []string{
		".sr-rating",
		"[aria-label*='star']",
		".c-stars",
		".rating-stars",
		"span[aria-label*='out of 5']",
		".visually-hidden:contains('out of')",
	}

	for _, selector := range ratingSelectors {
		rating := strings.TrimSpace(e.ChildText(selector))
		if rating != "" {
			return rating
		}
	}

	// Try to extract from aria-label
	ratingLabel := e.ChildAttr("span[aria-label*='star']", "aria-label")
	if ratingLabel == "" {
		ratingLabel = e.ChildAttr("span[aria-label*='out of 5']", "aria-label")
	}
	if ratingLabel != "" {
		return b.extractRatingFromText(ratingLabel)
	}

	return ""
}

func (b *BestBuyScraper) extractReviews(e *colly.HTMLElement) string {
	reviewSelectors := []string{
		".sr-review-count",
		"a[aria-label*='review']",
		".review-count",
		"span[aria-label*='review']",
		".c-reviews",
	}

	for _, selector := range reviewSelectors {
		reviews := strings.TrimSpace(e.ChildText(selector))
		if reviews != "" {
			return reviews
		}
	}

	// Try to extract from aria-label
	reviewLabel := e.ChildAttr("a[aria-label*='review']", "aria-label")
	if reviewLabel != "" {
		return b.extractReviewCountFromText(reviewLabel)
	}

	return ""
}

func (b *BestBuyScraper) formatPrice(price string) string {
	price = strings.TrimSpace(price)
	if price == "" {
		return ""
	}

	// If price already has $, return as is
	if strings.Contains(price, "$") {
		return price
	}

	// Extract numeric value
	numericPrice := regexp.MustCompile(`\d+\.?\d*`).FindString(price)
	if numericPrice == "" {
		return ""
	}

	return "$" + numericPrice
}

func (b *BestBuyScraper) extractPriceFromText(text string) string {
	priceRegex := regexp.MustCompile(`\$?\d+\.?\d*`)
	match := priceRegex.FindString(text)
	if match != "" {
		if !strings.HasPrefix(match, "$") {
			match = "$" + match
		}
		return match
	}
	return ""
}

func (b *BestBuyScraper) extractRatingFromText(text string) string {
	ratingRegex := regexp.MustCompile(`(\d+\.?\d*)\s*(?:out of|\/)\s*5`)
	matches := ratingRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1] + "/5"
	}
	return ""
}

func (b *BestBuyScraper) extractReviewCountFromText(text string) string {
	reviewRegex := regexp.MustCompile(`(\d+)\s*review`)
	matches := reviewRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1] + " reviews"
	}
	return ""
}

func (b *BestBuyScraper) isGenericTitle(title string) bool {
	genericTitles := []string{
		"Best Buy",
		"Shop Now",
		"Buy Now",
		"Add to Cart",
		"View Details",
		"Product",
		"Item",
		"Sale",
		"Special Offer",
	}

	titleLower := strings.ToLower(title)
	for _, generic := range genericTitles {
		if strings.Contains(titleLower, strings.ToLower(generic)) && len(title) < 20 {
			return true
		}
	}
	return false
}

func (b *BestBuyScraper) cleanProductName(name string) string {
	// Remove common Best Buy-specific text
	cleanPatterns := []string{
		`\s*\(.*?\)\s*$`, // Remove text in parentheses at the end
		`\s*-\s*Best Buy\s*$`,
		`\s*\|\s*Best Buy\s*$`,
		`\s*at Best Buy\s*$`,
		`\s*Best Buy\s*$`,
	}

	cleanName := name
	for _, pattern := range cleanPatterns {
		re := regexp.MustCompile(pattern)
		cleanName = re.ReplaceAllString(cleanName, "")
	}

	cleanName = strings.TrimSpace(cleanName)
	cleanName = regexp.MustCompile(`\s+`).ReplaceAllString(cleanName, " ")

	// Truncate if too long
	if len(cleanName) > 100 {
		cleanName = cleanName[:100] + "..."
	}

	return cleanName
}
