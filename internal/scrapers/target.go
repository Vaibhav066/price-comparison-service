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

type TargetScraper struct {
	collector *colly.Collector
}

func NewTargetScraper() *TargetScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("target.com", "www.target.com"),
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
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*target.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Target scraper error: %v", err)
	})

	return &TargetScraper{collector: c}
}

func (t *TargetScraper) Search(query, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	if strings.ToUpper(country) != "US" {
		log.Printf("Target: Country %s not supported, returning empty results", country)
		return products, nil
	}

	searchURL := t.getSearchURL(query)
	log.Printf("Searching Target (US) with URL: %s", searchURL)

	// Multiple selector strategies for Target's dynamic content
	selectors := []string{
		"[data-test='product-card']",
		"[data-test='@web/site-top-of-funnel/ProductCard']",
		".ProductCardImageWrapper",
		"section[data-test='product-card']",
		"div[data-test='product-card']",
		".h-full.flex.flex-col",
		"[data-test='product-title']",
	}

	foundAny := false
	errorCount := 0

	t.collector.OnResponse(func(r *colly.Response) {
		log.Printf("Target Response status: %d, Content-Length: %d", r.StatusCode, len(r.Body))
		bodyStr := string(r.Body)
		log.Printf("Page contains product data: %v", strings.Contains(bodyStr, "data-test") || strings.Contains(bodyStr, "product"))
	})

	for _, selector := range selectors {
		log.Printf("Trying Target selector: %s", selector)

		t.collector.OnHTML(selector, func(e *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    "Target US",
				Currency:  "USD",
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Extract name with multiple fallback selectors
			nameSelectors := []string{
				"[data-test='product-title']",
				"a[data-test='product-title']",
				".ProductCardImageWrapper h3",
				"h3 a",
				".styled__StyledLink-sc-1de6opt-0",
				"a[aria-label]",
				".h-text-sm",
				".h-text-bs",
			}

			for _, nameSelector := range nameSelectors {
				name := strings.TrimSpace(e.ChildText(nameSelector))
				if name == "" {
					// Try getting from aria-label or title attribute
					name = strings.TrimSpace(e.ChildAttr(nameSelector, "aria-label"))
					if name == "" {
						name = strings.TrimSpace(e.ChildAttr(nameSelector, "title"))
					}
				}

				if name != "" && len(name) > 5 && !t.isGenericTitle(name) {
					product.Name = t.cleanProductName(name)
					break
				}
			}

			if product.Name == "" {
				return // Skip if no valid name found
			}

			product.Price = t.extractPrice(e)
			product.URL = t.extractURL(e)
			product.Image = t.extractImage(e)
			product.Rating = t.extractRating(e)
			product.Reviews = t.extractReviews(e)

			if product.Price != "" {
				product.ID = fmt.Sprintf("target_us_%d", time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found Target product: %s - %s", product.Name, product.Price)
			}
		})

		err := t.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting Target with selector %s: %v", selector, err)
			errorCount++
			continue
		}

		// If we found products with this selector, break
		if foundAny {
			break
		}

		// Reset collector for next selector attempt
		t.collector = t.resetCollector()
		time.Sleep(2 * time.Second) // Additional delay between selector attempts
	}

	if !foundAny && errorCount == len(selectors) {
		log.Printf("Target: No products found and all selectors failed for query: %s", query)
		return products, fmt.Errorf("all Target scraping attempts failed")
	}

	if !foundAny {
		log.Printf("Target: No products found for query: %s", query)
	}

	log.Printf("Target found %d products", len(products))
	return products, nil
}

func (t *TargetScraper) resetCollector() *colly.Collector {
	c := colly.NewCollector(
		colly.AllowedDomains("target.com", "www.target.com"),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*target.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	return c
}

func (t *TargetScraper) getSearchURL(query string) string {
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	return fmt.Sprintf("https://www.target.com/s?searchTerm=%s", encodedQuery)
}

func (t *TargetScraper) extractPrice(e *colly.HTMLElement) string {
	priceSelectors := []string{
		"[data-test='product-price']",
		"span[data-test='product-price']",
		".price-current",
		".sr-price",
		"[aria-label*='current price']",
		"[aria-label*='$']",
		".h-text-red",
		".styled__CurrentPrice-sc-108xfm0-0",
		"span.h-text-sm.h-text-red",
		".h-display-flex span",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(e.ChildText(selector))
		if price != "" {
			formattedPrice := t.formatPrice(price)
			if formattedPrice != "" {
				return formattedPrice
			}
		}
	}

	// Try to extract price from aria-label
	priceFromLabel := e.ChildAttr("[aria-label*='$']", "aria-label")
	if priceFromLabel != "" {
		return t.extractPriceFromText(priceFromLabel)
	}

	return ""
}

func (t *TargetScraper) extractURL(e *colly.HTMLElement) string {
	urlSelectors := []string{
		"a[data-test='product-title']",
		"h3 a",
		".ProductCardImageWrapper a",
		"a[aria-label]",
		"a",
	}

	for _, selector := range urlSelectors {
		relativeURL := e.ChildAttr(selector, "href")
		if relativeURL != "" {
			if strings.HasPrefix(relativeURL, "http") {
				return relativeURL
			}
			if strings.HasPrefix(relativeURL, "/") {
				return "https://www.target.com" + relativeURL
			}
		}
	}

	return ""
}

func (t *TargetScraper) extractImage(e *colly.HTMLElement) string {
	imageSelectors := []string{
		"img[data-test='productImage']",
		"img[src*='target.scene7.com']",
		"img[alt*='product']",
		"picture img",
		"img",
	}

	for _, selector := range imageSelectors {
		imgSrc := e.ChildAttr(selector, "src")
		if imgSrc != "" && (strings.Contains(imgSrc, "target") || strings.Contains(imgSrc, "scene7")) {
			return imgSrc
		}

		// Try data-src for lazy loading
		imgSrc = e.ChildAttr(selector, "data-src")
		if imgSrc != "" && (strings.Contains(imgSrc, "target") || strings.Contains(imgSrc, "scene7")) {
			return imgSrc
		}
	}

	return ""
}

func (t *TargetScraper) extractRating(e *colly.HTMLElement) string {
	ratingSelectors := []string{
		"[data-test='rating']",
		"[aria-label*='star']",
		".sr-rating",
		".rating",
		"span[aria-label*='out of 5']",
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
		return t.extractRatingFromText(ratingLabel)
	}

	return ""
}

func (t *TargetScraper) extractReviews(e *colly.HTMLElement) string {
	reviewSelectors := []string{
		"[data-test='review-count']",
		"a[aria-label*='review']",
		".review-count",
		"span[aria-label*='review']",
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
		return t.extractReviewCountFromText(reviewLabel)
	}

	return ""
}

func (t *TargetScraper) formatPrice(price string) string {
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

func (t *TargetScraper) extractPriceFromText(text string) string {
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

func (t *TargetScraper) extractRatingFromText(text string) string {
	ratingRegex := regexp.MustCompile(`(\d+\.?\d*)\s*(?:out of|\/)\s*5`)
	matches := ratingRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1] + "/5"
	}
	return ""
}

func (t *TargetScraper) extractReviewCountFromText(text string) string {
	reviewRegex := regexp.MustCompile(`(\d+)\s*review`)
	matches := reviewRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1] + " reviews"
	}
	return ""
}

func (t *TargetScraper) isGenericTitle(title string) bool {
	genericTitles := []string{
		"Target",
		"Shop Now",
		"Buy Now",
		"Add to Cart",
		"View Details",
		"Product",
		"Item",
		"Sale",
	}

	titleLower := strings.ToLower(title)
	for _, generic := range genericTitles {
		if strings.Contains(titleLower, strings.ToLower(generic)) && len(title) < 20 {
			return true
		}
	}
	return false
}

func (t *TargetScraper) cleanProductName(name string) string {
	// Remove common Target-specific text
	cleanPatterns := []string{
		`\s*\(.*?\)\s*$`, // Remove text in parentheses at the end
		`\s*-\s*Target\.com\s*$`,
		`\s*\|\s*Target\s*$`,
		`\s*at Target\s*$`,
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
