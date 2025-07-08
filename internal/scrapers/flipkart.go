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

type FlipkartScraper struct {
	collector *colly.Collector
}

func NewFlipkartScraper() *FlipkartScraper {
	c := colly.NewCollector(
		colly.AllowedDomains("flipkart.com", "www.flipkart.com"),
		colly.Debugger(&debug.LogDebugger{}),
	)

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.5")
		r.Headers.Set("Referer", "https://www.flipkart.com/")
		r.Headers.Set("Cache-Control", "no-cache")
	})

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*flipkart.*",
		Parallelism: 1,
		Delay:       5 * time.Second,
	})

	return &FlipkartScraper{collector: c}
}

func (f *FlipkartScraper) Search(query string, country string) ([]models.Product, error) {
	// Always return empty slice instead of nil
	products := make([]models.Product, 0)

	if strings.ToUpper(country) != "IN" {
		log.Printf("Flipkart: Country %s not supported, returning empty results", country)
		return products, nil // Flipkart only works in India
	}

	searchURL := f.getSearchURL(query)
	log.Printf("Searching Flipkart (IN) with URL: %s", searchURL)

	selectors := []string{
		"[data-id]",
		"._1AtVbE",
		"._13oc-S",
	}

	foundAny := false

	f.collector.OnResponse(func(r *colly.Response) {
		log.Printf("Flipkart Response status: %d", r.StatusCode)
	})

	for _, selector := range selectors {
		f.collector.OnHTML(selector, func(e *colly.HTMLElement) {
			foundAny = true

			product := models.Product{
				Source:    "Flipkart",
				Currency:  "INR",
				ScrapedAt: time.Now(),
				InStock:   true,
			}

			// Extract name with multiple selectors
			nameSelectors := []string{"._4rR01T", ".s1Q9rs", "._2WkVRV"}
			for _, nameSelector := range nameSelectors {
				name := strings.TrimSpace(e.ChildText(nameSelector))
				if name == "" {
					continue
				}
				if name != "" && len(name) > 5 {
					product.Name = name
					break
				}
			}

			if product.Name == "" {
				return
			}

			product.Price = f.extractPrice(e)
			product.URL = f.extractURL(e)

			// Extract image
			for _, imgSelector := range []string{"._396cs4", "._2r_T1I"} {
				image := e.ChildAttr(imgSelector, "src")
				if image != "" {
					product.Image = image
					break
				}
			}

			if product.Price != "" {
				product.ID = fmt.Sprintf("flipkart_%d", time.Now().UnixNano())
				products = append(products, product)
				log.Printf("Found Flipkart product: %s - %s", product.Name, product.Price)
			}
		})

		err := f.collector.Visit(searchURL)
		if err != nil {
			log.Printf("Error visiting Flipkart: %v", err)
		}

		if foundAny {
			break
		}
	}

	if !foundAny {
		log.Printf("No Flipkart products found for query: %s", query)
	}

	log.Printf("Flipkart found %d products", len(products))
	return products, nil
}

func (f *FlipkartScraper) getSearchURL(query string) string {
	return fmt.Sprintf("https://www.flipkart.com/search?q=%s", strings.ReplaceAll(query, " ", "%20"))
}

func (f *FlipkartScraper) extractPrice(element *colly.HTMLElement) string {
	priceSelectors := []string{
		"._30jeq3",
		"._16Jk6d",
		"._1_WHN1",
		".s1Q9rs",
	}

	for _, selector := range priceSelectors {
		price := strings.TrimSpace(element.ChildText(selector))
		if price != "" {
			return f.formatPrice(price)
		}
	}

	return ""
}

func (f *FlipkartScraper) extractURL(element *colly.HTMLElement) string {
	relativeURL := element.ChildAttr("a", "href")
	if relativeURL != "" && !strings.HasPrefix(relativeURL, "http") {
		return "https://www.flipkart.com" + relativeURL
	}
	return relativeURL
}

func (f *FlipkartScraper) formatPrice(price string) string {
	price = strings.TrimSpace(price)
	if strings.Contains(price, "₹") {
		return price
	}

	numericPrice := regexp.MustCompile(`[^\d.,]`).ReplaceAllString(price, "")
	if numericPrice == "" {
		return price
	}
	return "₹" + numericPrice
}

func (f *FlipkartScraper) cleanFlipkartProductName(name string) string {
	genericTitles := []string{"Flipkart", "Shop Now", "Buy Now"}
	for _, generic := range genericTitles {
		if strings.Contains(name, generic) && len(name) < 20 {
			return ""
		}
	}

	cleanName := strings.TrimSpace(name)
	if len(cleanName) > 80 {
		cleanName = cleanName[:80] + "..."
	}
	return cleanName
}
