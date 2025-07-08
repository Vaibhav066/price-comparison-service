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

type AmazonScraper struct {
	collector *colly.Collector
}

func NewAmazonScraper() *AmazonScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("amazon.com", "www.amazon.com", "amazon.in", "www.amazon.in",
			"amazon.co.uk", "www.amazon.co.uk", "amazon.de", "www.amazon.de",
			"amazon.ca", "www.amazon.ca", "amazon.com.au", "www.amazon.com.au"),
		colly.Debugger(&debug.LogDebugger{}),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*amazon.*",
		Parallelism: 1,
		Delay:       2 * time.Second,
	})

	return &AmazonScraper{collector: c}
}

func (a *AmazonScraper) Search(query, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	searchURL := a.getSearchURL(query, country)
	log.Printf("Searching Amazon (%s) with URL: %s", country, searchURL)

	// Multiple selector strategies
	selectors := []string{
		"div[data-component-type='s-search-result']",
		"[data-component-type='s-search-result']",
		"div.s-result-item",
		"div[data-asin]",
		".s-search-result",
	}

	foundAny := false

	a.collector.OnResponse(func(r *colly.Response) {
		log.Printf("Amazon (%s) Response status: %d", country, r.StatusCode)
		bodyStr := string(r.Body)
		log.Printf("Page contains search results: %v", strings.Contains(bodyStr, "s-search-result"))
	})

	for _, selector := range selectors {
		log.Printf("Trying Amazon (%s) selector: %s", country, selector)

		a.collector.OnHTML(selector, func(e *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    fmt.Sprintf("Amazon %s", strings.ToUpper(country)),
				Currency:  a.getCurrencyForCountry(country),
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Try multiple name selectors
			nameSelectors := []string{
				"h2 a span",
				"h2.a-size-mini span",
				".s-size-mini span",
				"h2 span",
				".a-link-normal span",
			}

			for _, nameSelector := range nameSelectors {
				name := strings.TrimSpace(e.ChildText(nameSelector))
				if name != "" && len(name) > 5 {
					product.Name = name
					break
				}
			}

			if product.Name == "" {
				return // Skip if no valid name
			}

			product.Price = a.extractPrice(e, country)
			product.URL = a.extractURL(e, country)

			// Try multiple image selectors
			imageSelectors := []string{
				"img.s-image",
				".s-product-image-container img",
				"img[data-image-latency='s-product-image']",
				"img",
			}

			for _, imgSelector := range imageSelectors {
				image := e.ChildAttr(imgSelector, "src")
				if image != "" {
					product.Image = image
					break
				}
			}

			product.Rating = strings.TrimSpace(e.ChildText(".a-icon-alt"))
			product.Reviews = strings.TrimSpace(e.ChildText(".a-size-base"))

			if product.Price != "" {
				product.ID = fmt.Sprintf("amazon_%s_%d", country, time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found Amazon (%s) product: %s - %s", country, product.Name, product.Price)
			}
		})

		err := a.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting Amazon %s: %v", country, err)
		}

		if foundAny {
			break
		}

		// Reset collector for next selector
		a.collector = a.collector.Clone()
	}

	if !foundAny {
		log.Printf("No Amazon (%s) products found for query: %s", country, query)
	}

	log.Printf("Amazon %s found %d products", country, len(products))
	return products, nil
}

// Build country-specific search URLs
func (a *AmazonScraper) getSearchURL(query, country string) string {
	domains := map[string]string{
		"US": "https://www.amazon.com/s?k=%s",
		"IN": "https://www.amazon.in/s?k=%s",
		"UK": "https://www.amazon.co.uk/s?k=%s",
		"DE": "https://www.amazon.de/s?k=%s",
		"CA": "https://www.amazon.ca/s?k=%s",
		"AU": "https://www.amazon.com.au/s?k=%s",
		"FR": "https://www.amazon.fr/s?k=%s",
		"IT": "https://www.amazon.it/s?k=%s",
		"ES": "https://www.amazon.es/s?k=%s",
		"JP": "https://www.amazon.co.jp/s?k=%s",
	}

	baseURL := domains[strings.ToUpper(country)]
	if baseURL == "" {
		baseURL = domains["US"] // fallback
	}

	return fmt.Sprintf(baseURL, strings.ReplaceAll(query, " ", "+"))
}

func (a *AmazonScraper) getCurrencyForCountry(country string) string {
	currencies := map[string]string{
		"US": "USD", "CA": "CAD", "IN": "INR", "UK": "GBP",
		"DE": "EUR", "FR": "EUR", "IT": "EUR", "ES": "EUR",
		"AU": "AUD", "JP": "JPY",
	}

	if currency, exists := currencies[strings.ToUpper(country)]; exists {
		return currency
	}
	return "USD"
}

func (a *AmazonScraper) extractPrice(e *colly.HTMLElement, country string) string {
	priceSelectors := []string{
		".a-price-whole",
		".a-price .a-offscreen",
		".a-price-fraction",
		".a-price-symbol",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(e.ChildText(selector))
		if price != "" {
			return a.formatPriceForCountry(price, country)
		}
	}

	return ""
}

func (a *AmazonScraper) extractURL(e *colly.HTMLElement, country string) string {
	relativeURL := e.ChildAttr("h2 a", "href")
	if relativeURL != "" {
		baseURL := a.getBaseURL(country)
		return baseURL + relativeURL
	}
	return ""
}

func (a *AmazonScraper) getBaseURL(country string) string {
	baseURLs := map[string]string{
		"US": "https://www.amazon.com",
		"IN": "https://www.amazon.in",
		"UK": "https://www.amazon.co.uk",
		"DE": "https://www.amazon.de",
		"CA": "https://www.amazon.ca",
		"AU": "https://www.amazon.com.au",
	}

	if baseURL, exists := baseURLs[strings.ToUpper(country)]; exists {
		return baseURL
	}
	return "https://www.amazon.com"
}

func (a *AmazonScraper) formatPriceForCountry(price, country string) string {
	// Clean up the price string
	price = strings.TrimSpace(price)
	price = regexp.MustCompile(`[^\d.,]`).ReplaceAllString(price, "")

	currency := a.getCurrencyForCountry(country)

	switch currency {
	case "INR":
		return "₹" + price
	case "GBP":
		return "£" + price
	case "EUR":
		return "€" + price
	case "JPY":
		return "¥" + price
	default:
		return "$" + price
	}
}
