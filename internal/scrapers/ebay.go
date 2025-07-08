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

type EbayScraper struct {
	collector *colly.Collector
}

func NewEbayScraper() *EbayScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("ebay.com", "www.ebay.com", "ebay.co.uk", "www.ebay.co.uk",
			"ebay.de", "www.ebay.de", "ebay.ca", "www.ebay.ca", "ebay.com.au", "www.ebay.com.au",
			"ebay.fr", "www.ebay.fr", "ebay.it", "www.ebay.it"),
		colly.Debugger(&debug.LogDebugger{}),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
		r.Headers.Set("Accept-Encoding", "gzip, deflate")
		r.Headers.Set("Cache-Control", "no-cache")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*ebay.*",
		Parallelism: 1,
		Delay:       2 * time.Second,
	})

	return &EbayScraper{collector: c}
}

func (e *EbayScraper) Search(query string, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	searchURL := e.getSearchURL(query, country)
	log.Printf("Searching eBay (%s) with URL: %s", country, searchURL)

	selectors := []string{
		".s-item",
		"div.s-item",
		"[data-view='mi:1686|iid:1']",
	}

	foundAny := false

	e.collector.OnResponse(func(r *colly.Response) {
		log.Printf("eBay (%s) Response status: %d", country, r.StatusCode)
		bodyStr := string(r.Body)
		log.Printf("Page contains 's-item': %v", strings.Contains(bodyStr, "s-item"))
	})

	for _, selector := range selectors {
		log.Printf("Trying eBay (%s) selector: %s", country, selector)

		e.collector.OnHTML(selector, func(element *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    fmt.Sprintf("eBay %s", country),
				Currency:  e.getCurrencyForCountry(country),
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Extract product details
			product.Name = e.cleanEbayProductName(strings.TrimSpace(element.ChildText("h3.s-item__title, .s-item__title")))
			if product.Name == "" {
				return // Skip if no valid name
			}

			product.Price = e.extractPrice(element, country)
			product.URL = e.extractURL(element, country)
			product.Image = element.ChildAttr("img", "src")
			product.Rating = strings.TrimSpace(element.ChildText(".ebay-review-stars"))
			product.Reviews = strings.TrimSpace(element.ChildText(".s-item__reviews-count"))

			if product.Price != "" {
				product.ID = fmt.Sprintf("ebay_%s_%d", country, time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found eBay (%s) product: %s - %s", country, product.Name, product.Price)
			}
		})

		err := e.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting eBay (%s): %v", country, err)
		}

		if foundAny {
			break
		}

		// Reset collector for next selector
		e.collector = e.collector.Clone()
	}

	if !foundAny {
		log.Printf("No eBay (%s) products found for query: %s", country, query)
	}

	log.Printf("eBay (%s) found %d products", country, len(products))
	return products, nil
}

func (e *EbayScraper) getSearchURL(query, country string) string {
	domains := map[string]string{
		"US": "https://www.ebay.com/sch/i.html?_nkw=%s&_sacat=0",
		"UK": "https://www.ebay.co.uk/sch/i.html?_nkw=%s&_sacat=0",
		"DE": "https://www.ebay.de/sch/i.html?_nkw=%s&_sacat=0",
		"CA": "https://www.ebay.ca/sch/i.html?_nkw=%s&_sacat=0",
		"AU": "https://www.ebay.com.au/sch/i.html?_nkw=%s&_sacat=0",
		"FR": "https://www.ebay.fr/sch/i.html?_nkw=%s&_sacat=0",
		"IT": "https://www.ebay.it/sch/i.html?_nkw=%s&_sacat=0",
		"IN": "https://www.ebay.com/sch/i.html?_nkw=%s&_sacat=0",
	}

	baseURL := domains[country]
	if baseURL == "" {
		baseURL = domains["US"] // fallback to US
	}

	return fmt.Sprintf(baseURL, strings.ReplaceAll(query, " ", "+"))
}

func (e *EbayScraper) getCurrencyForCountry(country string) string {
	currencies := map[string]string{
		"US": "USD",
		"UK": "GBP",
		"DE": "EUR",
		"CA": "CAD",
		"AU": "AUD",
		"FR": "EUR",
		"IT": "EUR",
		"IN": "INR",
	}

	if currency, exists := currencies[country]; exists {
		return currency
	}
	return "USD"
}

func (e *EbayScraper) extractPrice(element *colly.HTMLElement, country string) string {
	priceSelectors := []string{
		".s-item__price .notranslate",
		".s-item__price",
		".s-item__detail .s-item__price",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(element.ChildText(selector))
		if price != "" {
			return e.formatPriceForCountry(price, country)
		}
	}

	return ""
}

func (e *EbayScraper) extractURL(element *colly.HTMLElement, country string) string {
	url := element.ChildAttr("h3.s-item__title a, .s-item__title a", "href")
	if url == "" {
		url = element.ChildAttr("a", "href")
	}
	return url
}

func (e *EbayScraper) formatPriceForCountry(price, country string) string {
	// Clean up the price string
	price = strings.TrimSpace(price)

	currency := e.getCurrencyForCountry(country)

	// If price already has currency symbol, return as is
	if strings.Contains(price, "$") || strings.Contains(price, "£") ||
		strings.Contains(price, "€") || strings.Contains(price, "C$") ||
		strings.Contains(price, "A$") {
		return price
	}

	// Extract numeric value and add appropriate currency
	numericPrice := regexp.MustCompile(`[^\d.,]`).ReplaceAllString(price, "")
	if numericPrice == "" {
		return price
	}

	switch currency {
	case "GBP":
		return "£" + numericPrice
	case "EUR":
		return "€" + numericPrice
	case "CAD":
		return "C$" + numericPrice
	case "AUD":
		return "A$" + numericPrice
	default:
		return "$" + numericPrice
	}
}

func (e *EbayScraper) cleanEbayProductName(name string) string {
	// Skip generic eBay titles
	genericTitles := []string{
		"Shop on eBay",
		"New Listing",
		"SPONSORED",
		"eBay",
	}

	for _, generic := range genericTitles {
		if strings.Contains(name, generic) && len(name) < 20 {
			return "" // This will cause the product to be skipped
		}
	}

	// Remove common eBay-specific text
	cleanPatterns := []string{
		`New Listing`,
		`SPONSORED`,
		`Free shipping`,
		`Best Offer`,
		`Buy It Now`,
	}

	cleanName := name
	for _, pattern := range cleanPatterns {
		cleanName = strings.ReplaceAll(cleanName, pattern, "")
	}

	cleanName = strings.TrimSpace(cleanName)
	cleanName = regexp.MustCompile(`\s+`).ReplaceAllString(cleanName, " ")

	if len(cleanName) > 80 {
		cleanName = cleanName[:80] + "..."
	}

	return cleanName
}
