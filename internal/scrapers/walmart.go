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

type WalmartScraper struct {
	collector *colly.Collector
}

func NewWalmartScraper() *WalmartScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("walmart.com", "www.walmart.com"),
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
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*walmart.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Walmart scraper error: %v", err)
	})

	return &WalmartScraper{collector: c}
}

func (w *WalmartScraper) Search(query, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	if strings.ToUpper(country) != "US" {
		log.Printf("Walmart: Country %s not supported, returning empty results", country)
		return products, nil
	}

	searchURL := w.getSearchURL(query)
	log.Printf("Searching Walmart (US) with URL: %s", searchURL)

	// Multiple selector strategies for robustness
	selectors := []string{
		"[data-testid='item']",
		"[data-automation-id='product-title']",
		".search-result-gridview-item",
		"[data-testid='list-view'] > div",
		".mb0.ph1.pa0-xl.bb.b--near-white.w-25",
		".search-result-listview-item",
	}

	foundAny := false
	errorCount := 0

	w.collector.OnResponse(func(r *colly.Response) {
		log.Printf("Walmart Response status: %d, Content-Length: %d", r.StatusCode, len(r.Body))
		bodyStr := string(r.Body)
		log.Printf("Page contains product data: %v", strings.Contains(bodyStr, "data-testid") || strings.Contains(bodyStr, "search-result"))
	})

	for _, selector := range selectors {
		log.Printf("Trying Walmart selector: %s", selector)

		w.collector.OnHTML(selector, func(e *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    "Walmart US",
				Currency:  "USD",
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Extract name with multiple fallback selectors
			nameSelectors := []string{
				"[data-automation-id='product-title']",
				"span[data-automation-id='product-title']",
				".normal.dark-gray.mb1",
				"h3 a span",
				".f6.f5-l.lh-title.dark-gray.mv1",
				"a[data-testid='product-title']",
				".w_DJ",
			}

			for _, nameSelector := range nameSelectors {
				name := strings.TrimSpace(e.ChildText(nameSelector))
				if name != "" && len(name) > 5 && !w.isGenericTitle(name) {
					product.Name = w.cleanProductName(name)
					break
				}
			}

			if product.Name == "" {
				return // Skip if no valid name found
			}

			product.Price = w.extractPrice(e)
			product.URL = w.extractURL(e)
			product.Image = w.extractImage(e)
			product.Rating = w.extractRating(e)
			product.Reviews = w.extractReviews(e)

			if product.Price != "" {
				product.ID = fmt.Sprintf("walmart_us_%d", time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found Walmart product: %s - %s", product.Name, product.Price)
			}
		})

		err := w.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting Walmart with selector %s: %v", selector, err)
			errorCount++
			continue
		}

		// If we found products with this selector, break
		if foundAny {
			break
		}

		// Reset collector for next selector attempt
		w.collector = w.resetCollector()
		time.Sleep(2 * time.Second) // Additional delay between selector attempts
	}

	if !foundAny && errorCount == len(selectors) {
		log.Printf("Walmart: No products found and all selectors failed for query: %s", query)
		return products, fmt.Errorf("all Walmart scraping attempts failed")
	}

	if !foundAny {
		log.Printf("Walmart: No products found for query: %s", query)
	}

	log.Printf("Walmart found %d products", len(products))
	return products, nil
}

func (w *WalmartScraper) resetCollector() *colly.Collector {
	c := colly.NewCollector(
		colly.AllowedDomains("walmart.com", "www.walmart.com"),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*walmart.*",
		Parallelism: 1,
		Delay:       3 * time.Second,
	})

	return c
}

func (w *WalmartScraper) getSearchURL(query string) string {
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	return fmt.Sprintf("https://www.walmart.com/search?q=%s", encodedQuery)
}

func (w *WalmartScraper) extractPrice(e *colly.HTMLElement) string {
	priceSelectors := []string{
		"[itemprop='price']",
		"span[itemprop='price']",
		".price-current",
		".sr-price .visuallyhidden",
		"[data-automation-id='product-price']",
		".f2.b.dark-gray",
		".price-group .price-current",
		".arrange-fit.arrange-fill",
		".price.display-inline-block.arrange-fit",
		"span.price",
		"[aria-label*='current price']",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(e.ChildText(selector))
		if price != "" {
			formattedPrice := w.formatPrice(price)
			if formattedPrice != "" {
				return formattedPrice
			}
		}
	}

	// Try to extract price from aria-label
	priceFromLabel := e.ChildAttr("[aria-label*='current price']", "aria-label")
	if priceFromLabel != "" {
		return w.extractPriceFromText(priceFromLabel)
	}

	return ""
}

func (w *WalmartScraper) extractURL(e *colly.HTMLElement) string {
	urlSelectors := []string{
		"a[data-testid='product-title']",
		"h3 a",
		"a[data-automation-id='product-title']",
		"a",
	}

	for _, selector := range urlSelectors {
		relativeURL := e.ChildAttr(selector, "href")
		if relativeURL != "" {
			if strings.HasPrefix(relativeURL, "http") {
				return relativeURL
			}
			if strings.HasPrefix(relativeURL, "/") {
				return "https://www.walmart.com" + relativeURL
			}
		}
	}

	return ""
}

func (w *WalmartScraper) extractImage(e *colly.HTMLElement) string {
	imageSelectors := []string{
		"img[data-testid='productTileImage']",
		"img[src*='i5.walmartimages.com']",
		"img[alt*='product']",
		"img",
	}

	for _, selector := range imageSelectors {
		imgSrc := e.ChildAttr(selector, "src")
		if imgSrc != "" && strings.Contains(imgSrc, "walmart") {
			return imgSrc
		}
	}

	return ""
}

func (w *WalmartScraper) extractRating(e *colly.HTMLElement) string {
	ratingSelectors := []string{
		".average-rating",
		"[data-testid='reviews-rating']",
		".stars-reviews-count-node",
		"span[aria-label*='star']",
		".review-stars",
	}

	for _, selector := range ratingSelectors {
		rating := strings.TrimSpace(e.ChildText(selector))
		if rating != "" {
			return rating
		}
	}

	// Try to extract from aria-label
	ratingLabel := e.ChildAttr("span[aria-label*='star']", "aria-label")
	if ratingLabel != "" {
		return w.extractRatingFromText(ratingLabel)
	}

	return ""
}

func (w *WalmartScraper) extractReviews(e *colly.HTMLElement) string {
	reviewSelectors := []string{
		"[data-testid='reviews-count']",
		".reviews-count",
		"span[aria-label*='review']",
	}

	for _, selector := range reviewSelectors {
		reviews := strings.TrimSpace(e.ChildText(selector))
		if reviews != "" {
			return reviews
		}
	}

	return ""
}

func (w *WalmartScraper) formatPrice(price string) string {
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

func (w *WalmartScraper) extractPriceFromText(text string) string {
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

func (w *WalmartScraper) extractRatingFromText(text string) string {
	ratingRegex := regexp.MustCompile(`(\d+\.?\d*)\s*(?:out of|\/)\s*5`)
	matches := ratingRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1] + "/5"
	}
	return ""
}

func (w *WalmartScraper) isGenericTitle(title string) bool {
	genericTitles := []string{
		"Walmart",
		"Shop Now",
		"Buy Now",
		"Add to Cart",
		"View Details",
		"Product",
		"Item",
	}

	titleLower := strings.ToLower(title)
	for _, generic := range genericTitles {
		if strings.Contains(titleLower, strings.ToLower(generic)) && len(title) < 20 {
			return true
		}
	}
	return false
}

func (w *WalmartScraper) cleanProductName(name string) string {
	// Remove common Walmart-specific text
	cleanPatterns := []string{
		`\s*\(.*?\)\s*$`, // Remove text in parentheses at the end
		`\s*-\s*Walmart\.com\s*$`,
		`\s*\|\s*Walmart\s*$`,
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
