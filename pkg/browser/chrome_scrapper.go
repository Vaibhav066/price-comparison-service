package browser

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"price-comparison-api/internal/models"
)

type ChromeScraper struct {
	ctx           context.Context
	allocCancel   context.CancelFunc
	timeoutCancel context.CancelFunc
	cancel        context.CancelFunc
}

type ShoppingSite struct {
	URL  string
	Name string
}

func NewChromeScraper() *ChromeScraper {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.ExecPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	// Create context without timeout in constructor
	ctx, cancel := chromedp.NewContext(allocCtx)

	return &ChromeScraper{
		ctx:         ctx,
		allocCancel: allocCancel,
		cancel:      cancel,
	}
}

func (c *ChromeScraper) SearchUniversal(query, country string) ([]models.Product, error) {
	if c == nil {
		log.Printf("Chrome scraper not available, skipping")
		return []models.Product{}, nil
	}

	log.Printf("Chrome scraper: searching for '%s' in %s", query, country)

	var allProducts []models.Product

	// Direct site scraping strategy
	sites := c.getShoppingSites(query, country)

	for _, site := range sites {
		if len(allProducts) >= 10 { // Limit total products
			break
		}

		products := c.scrapeDirectly(site.URL, site.Name, query, country)
		allProducts = append(allProducts, products...)

		// Add delay between sites
		time.Sleep(2 * time.Second)
	}

	log.Printf("Chrome scraper: found %d products", len(allProducts))
	return allProducts, nil
}

func (c *ChromeScraper) getShoppingSites(query, country string) []ShoppingSite {
	encodedQuery := url.QueryEscape(query)

	sites := []ShoppingSite{}

	// Country-specific sites
	switch strings.ToUpper(country) {
	case "US":
		sites = []ShoppingSite{
			{fmt.Sprintf("https://www.amazon.com/s?k=%s", encodedQuery), "Amazon"},
			{fmt.Sprintf("https://www.ebay.com/sch/i.html?_nkw=%s", encodedQuery), "eBay"},
			{fmt.Sprintf("https://www.walmart.com/search/?query=%s", encodedQuery), "Walmart"},
		}
	case "IN":
		sites = []ShoppingSite{
			{fmt.Sprintf("https://www.amazon.in/s?k=%s", encodedQuery), "Amazon India"},
			{fmt.Sprintf("https://www.flipkart.com/search?q=%s", encodedQuery), "Flipkart"},
			{fmt.Sprintf("https://www.myntra.com/search?q=%s", encodedQuery), "Myntra"},
		}
	case "UK":
		sites = []ShoppingSite{
			{fmt.Sprintf("https://www.amazon.co.uk/s?k=%s", encodedQuery), "Amazon UK"},
			{fmt.Sprintf("https://www.ebay.co.uk/sch/i.html?_nkw=%s", encodedQuery), "eBay UK"},
		}
	default:
		sites = []ShoppingSite{
			{fmt.Sprintf("https://www.amazon.com/s?k=%s", encodedQuery), "Amazon"},
			{fmt.Sprintf("https://www.ebay.com/sch/i.html?_nkw=%s", encodedQuery), "eBay"},
		}
	}

	return sites
}

func (c *ChromeScraper) scrapeDirectly(siteURL, siteName, query, country string) []models.Product {
	var products []models.Product

	log.Printf("Chrome: Scraping %s at %s", siteName, siteURL)

	// Create a timeout context only for this specific scrape
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Create a new browser context for this scrape
	taskCtx, taskCancel := chromedp.NewContext(ctx)
	defer taskCancel()

	// Navigate and wait for page to load
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(siteURL),
		chromedp.Sleep(3*time.Second),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	)

	if err != nil {
		log.Printf("Chrome: Navigation error for %s: %v", siteName, err)
		return products
	}

	log.Printf("Chrome: Successfully loaded %s", siteName)

	// Extract products based on site
	if strings.Contains(siteName, "Amazon") {
		products = c.extractAmazonProductsWithContext(taskCtx, query, country)
	} else if strings.Contains(siteName, "eBay") {
		products = c.extractEbayProductsWithContext(taskCtx, query, country)
	} else if strings.Contains(siteName, "Walmart") {
		products = c.extractWalmartProductsWithContext(taskCtx, query, country)
	}

	log.Printf("Chrome: Found %d products from %s", len(products), siteName)
	return products
}

func (c *ChromeScraper) extractAmazonProductsWithContext(ctx context.Context, query, country string) []models.Product {
	log.Printf("Chrome Amazon extraction temporarily disabled")
	return []models.Product{}
}

func (c *ChromeScraper) extractEbayProductsWithContext(ctx context.Context, query, country string) []models.Product {
	log.Printf("Chrome eBay extraction temporarily disabled")
	return []models.Product{}
}

func (c *ChromeScraper) extractWalmartProductsWithContext(ctx context.Context, query, country string) []models.Product {
	log.Printf("Chrome Walmart extraction temporarily disabled")
	return []models.Product{}
}

func (c *ChromeScraper) findRelevantSites(query, country string) []string {
	var links []string

	// Create a Google search query for shopping sites
	searchQuery := fmt.Sprintf("%s buy online %s site:amazon.com OR site:ebay.com OR site:flipkart.com OR site:myntra.com OR site:snapdeal.com", query, country)
	googleURL := fmt.Sprintf("https://www.google.com/search?q=%s",
		url.QueryEscape(searchQuery))

	log.Printf("Chrome: Searching Google with: %s", googleURL)

	err := chromedp.Run(c.ctx,
		chromedp.Navigate(googleURL),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('a[href*="amazon.com"], a[href*="ebay.com"], a[href*="flipkart.com"], a[href*="myntra.com"], a[href*="snapdeal.com"]'))
				.slice(0, 5)
				.map(a => a.href)
				.filter(href => href.includes('/dp/') || href.includes('/itm/') || href.includes('/p/'))
		`, &links),
	)

	if err != nil {
		log.Printf("Chrome: Error finding sites: %v", err)
		return []string{}
	}

	log.Printf("Chrome: Found %d relevant product links", len(links))
	return links
}

func (c *ChromeScraper) extractAmazonProducts(query, country string) []models.Product {
	var products []models.Product

	var productData []map[string]string
	err := chromedp.Run(c.ctx,
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('[data-component-type="s-search-result"]')).slice(0, 3).map(item => {
				const title = item.querySelector('h2 a span')?.textContent?.trim() || '';
				const price = item.querySelector('.a-price-whole')?.textContent?.trim() || '';
				const image = item.querySelector('img.s-image')?.src || '';
				const url = item.querySelector('h2 a')?.href || '';
				return {title, price, image, url};
			}).filter(item => item.title && item.price)
		`, &productData),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting Amazon products: %v", err)
		return products
	}

	for _, data := range productData {
		if c.isRelevantProduct(data["title"], query) {
			product := models.Product{
				ID:        fmt.Sprintf("chrome_amazon_%d", time.Now().UnixNano()),
				Name:      data["title"],
				Price:     c.cleanPrice(data["price"], country),
				Currency:  c.getCurrencyForCountry(country),
				URL:       data["url"],
				Image:     data["image"],
				Source:    "Amazon (Chrome)",
				ScrapedAt: time.Now(),
				InStock:   true,
			}
			products = append(products, product)
		}
	}

	return products
}

func (c *ChromeScraper) extractEbayProducts(query, country string) []models.Product {
	var products []models.Product

	var productData []map[string]string
	err := chromedp.Run(c.ctx,
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('.s-item')).slice(0, 3).map(item => {
				const title = item.querySelector('.s-item__title')?.textContent?.trim() || '';
				const price = item.querySelector('.s-item__price')?.textContent?.trim() || '';
				const image = item.querySelector('img')?.src || '';
				const url = item.querySelector('.s-item__title a')?.href || '';
				return {title, price, image, url};
			}).filter(item => item.title && item.price && !item.title.includes('Shop on eBay'))
		`, &productData),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting eBay products: %v", err)
		return products
	}

	for _, data := range productData {
		if c.isRelevantProduct(data["title"], query) {
			product := models.Product{
				ID:        fmt.Sprintf("chrome_ebay_%d", time.Now().UnixNano()),
				Name:      data["title"],
				Price:     c.cleanPrice(data["price"], country),
				Currency:  c.getCurrencyForCountry(country),
				URL:       data["url"],
				Image:     data["image"],
				Source:    "eBay (Chrome)",
				ScrapedAt: time.Now(),
				InStock:   true,
			}
			products = append(products, product)
		}
	}

	return products
}

func (c *ChromeScraper) extractFlipkartProducts(query, country string) []models.Product {
	var products []models.Product

	var productData []map[string]string
	err := chromedp.Run(c.ctx,
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('[data-id]')).slice(0, 3).map(item => {
				const title = item.querySelector('._4rR01T')?.textContent?.trim() || 
							  item.querySelector('.s1Q9rs')?.textContent?.trim() || '';
				const price = item.querySelector('._30jeq3')?.textContent?.trim() || 
							  item.querySelector('._1_WHN1')?.textContent?.trim() || '';
				const image = item.querySelector('._396cs4')?.src || '';
				const url = item.querySelector('a')?.href || '';
				return {title, price, image, url};
			}).filter(item => item.title && item.price)
		`, &productData),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting Flipkart products: %v", err)
		return products
	}

	for _, data := range productData {
		if c.isRelevantProduct(data["title"], query) {
			product := models.Product{
				ID:        fmt.Sprintf("chrome_flipkart_%d", time.Now().UnixNano()),
				Name:      data["title"],
				Price:     c.cleanPrice(data["price"], country),
				Currency:  "INR",
				URL:       c.makeAbsoluteURL(data["url"], "https://www.flipkart.com"),
				Image:     data["image"],
				Source:    "Flipkart (Chrome)",
				ScrapedAt: time.Now(),
				InStock:   true,
			}
			products = append(products, product)
		}
	}

	return products
}

func (c *ChromeScraper) extractMyntraProducts(query, country string) []models.Product {
	var products []models.Product

	var productData []map[string]string
	err := chromedp.Run(c.ctx,
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('.product-base')).slice(0, 3).map(item => {
				const title = item.querySelector('.product-brand, .product-product')?.textContent?.trim() || '';
				const price = item.querySelector('.product-discountedPrice')?.textContent?.trim() || '';
				const image = item.querySelector('.product-imageSlider img')?.src || '';
				const link = item.querySelector('a')?.href || '';
				return { title, price, image, link };
			});
		`, &productData),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting Myntra products: %v", err)
		return products
	}

	for _, data := range productData {
		if c.isRelevantProduct(data["title"], query) {
			product := models.Product{
				ID:        fmt.Sprintf("myntra_%d", time.Now().UnixNano()),
				Name:      data["title"],
				Price:     data["price"],
				URL:       data["link"],
				Image:     data["image"],
				Source:    "Myntra (Chrome)",
				Currency:  "INR",
				ScrapedAt: time.Now(),
				InStock:   true,
			}
			products = append(products, product)
		}
	}

	return products
}

func (c *ChromeScraper) extractWalmartProducts(query, country string) []models.Product {
	var products []models.Product

	var productData []map[string]string
	err := chromedp.Run(c.ctx,
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('[data-testid="item"]')).slice(0, 3).map(item => {
				const title = item.querySelector('[data-automation-id="product-title"]')?.textContent?.trim() || '';
				const price = item.querySelector('[itemprop="price"]')?.textContent?.trim() || '';
				const image = item.querySelector('img')?.src || '';
				const link = item.querySelector('a')?.href || '';
				return { title, price, image, link };
			});
		`, &productData),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting Walmart products: %v", err)
		return products
	}

	for _, data := range productData {
		if c.isRelevantProduct(data["title"], query) {
			product := models.Product{
				ID:        fmt.Sprintf("walmart_%d", time.Now().UnixNano()),
				Name:      data["title"],
				Price:     data["price"],
				URL:       data["link"],
				Image:     data["image"],
				Source:    "Walmart (Chrome)",
				Currency:  "USD",
				ScrapedAt: time.Now(),
				InStock:   true,
			}
			products = append(products, product)
		}
	}

	return products
}

func (c *ChromeScraper) extractFromSite(siteURL, query, country string) []models.Product {
	var products []models.Product

	log.Printf("Chrome: Extracting from %s", siteURL)

	var title, price, image string

	err := chromedp.Run(c.ctx,
		chromedp.Navigate(siteURL),
		chromedp.Sleep(2*time.Second),

		// Try multiple selectors for product title
		chromedp.Evaluate(`
			document.querySelector('#productTitle, .x-item-title-label, ._2B_pmu, .pdp-mod-product-badge-title, h1')?.textContent?.trim() || ''
		`, &title),

		// Try multiple selectors for price
		chromedp.Evaluate(`
			document.querySelector('.a-price-whole, .notranslate, ._1_WHN1, .pdp-price, .price')?.textContent?.trim() || ''
		`, &price),

		// Try multiple selectors for image
		chromedp.Evaluate(`
			document.querySelector('#landingImage, .s-image, ._396cs4, .pdp-mod-common-image img, .product-image img')?.src || ''
		`, &image),
	)

	if err != nil {
		log.Printf("Chrome: Error extracting from %s: %v", siteURL, err)
		return products
	}

	// Validate extracted data
	if title == "" || len(title) < 5 {
		log.Printf("Chrome: No valid title found for %s", siteURL)
		return products
	}

	if !c.isRelevantProduct(title, query) {
		log.Printf("Chrome: Product not relevant: %s", title)
		return products
	}

	product := models.Product{
		ID:        fmt.Sprintf("chrome_%d", time.Now().UnixNano()),
		Name:      title,
		Price:     c.cleanPrice(price, country),
		Currency:  c.getCurrencyForCountry(country),
		URL:       siteURL,
		Image:     c.makeAbsoluteURL(image, siteURL),
		Source:    c.getSourceName(siteURL),
		ScrapedAt: time.Now(),
		InStock:   true,
	}

	if product.Price != "" {
		products = append(products, product)
		log.Printf("Chrome: Found product: %s - %s", product.Name, product.Price)
	}

	return products
}

func (c *ChromeScraper) isRelevantProduct(title, query string) bool {
	if title == "" {
		return false
	}

	titleLower := strings.ToLower(title)
	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	// Check if at least one query word appears in the title
	for _, word := range queryWords {
		if strings.Contains(titleLower, word) {
			return true
		}
	}

	return false
}

func (c *ChromeScraper) getSourceName(siteURL string) string {
	if strings.Contains(siteURL, "amazon") {
		return "Amazon (Chrome)"
	}
	if strings.Contains(siteURL, "ebay") {
		return "eBay (Chrome)"
	}
	if strings.Contains(siteURL, "flipkart") {
		return "Flipkart (Chrome)"
	}
	if strings.Contains(siteURL, "myntra") {
		return "Myntra (Chrome)"
	}
	if strings.Contains(siteURL, "snapdeal") {
		return "Snapdeal (Chrome)"
	}

	if u, err := url.Parse(siteURL); err == nil {
		return fmt.Sprintf("%s (Chrome)", u.Host)
	}

	return "Unknown (Chrome)"
}

func (c *ChromeScraper) getCurrencyForCountry(country string) string {
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

func (c *ChromeScraper) cleanPrice(price, country string) string {
	if price == "" {
		return ""
	}

	// Remove extra whitespace and clean up
	price = strings.TrimSpace(price)

	// If price already has a currency symbol, return as is
	if strings.ContainsAny(price, "$£€₹¥") {
		return price
	}

	// Add currency symbol based on country
	currency := c.getCurrencyForCountry(country)
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
func (c *ChromeScraper) makeAbsoluteURL(baseURL, relativeURL string) string {
	if relativeURL == "" {
		return ""
	}

	// If it's already absolute, return as is
	if strings.HasPrefix(relativeURL, "http://") || strings.HasPrefix(relativeURL, "https://") {
		return relativeURL
	}

	// Parse the base URL
	base, err := url.Parse(baseURL)
	if err != nil {
		log.Printf("Error parsing base URL %s: %v", baseURL, err)
		return relativeURL
	}

	// Parse the relative URL
	rel, err := url.Parse(relativeURL)
	if err != nil {
		log.Printf("Error parsing relative URL %s: %v", relativeURL, err)
		return relativeURL
	}

	// Resolve the relative URL against the base
	resolved := base.ResolveReference(rel)
	return resolved.String()
}

func (c *ChromeScraper) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *ChromeScraper) debugCurrentPage(siteName string) {
	var title, url string
	var bodyLength int

	err := chromedp.Run(c.ctx,
		chromedp.Title(&title),
		chromedp.Location(&url),
		chromedp.Evaluate(`document.body.innerHTML.length`, &bodyLength),
	)

	if err != nil {
		log.Printf("Chrome debug error for %s: %v", siteName, err)
		return
	}

	log.Printf("Chrome debug - Site: %s, Title: %s, URL: %s, Body length: %d",
		siteName, title, url, bodyLength)
}
