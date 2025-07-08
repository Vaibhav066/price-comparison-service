package services

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"price-comparison-api/internal/models"
	"price-comparison-api/internal/scrapers"
	"price-comparison-api/pkg/browser"
	"price-comparison-api/pkg/cache"
	"price-comparison-api/pkg/utils"
)

type SearchService struct {
	amazonScraper   *scrapers.AmazonScraper
	ebayScraper     *scrapers.EbayScraper
	flipkartScraper *scrapers.FlipkartScraper
	walmartScraper  *scrapers.WalmartScraper
	targetScraper   *scrapers.TargetScraper
	bestBuyScraper  *scrapers.BestBuyScraper
	chromeScraper   *browser.ChromeScraper
	cache           *cache.RedisCache
}

func NewSearchService() *SearchService {
	return &SearchService{
		amazonScraper:   scrapers.NewAmazonScraper(),
		ebayScraper:     scrapers.NewEbayScraper(),
		flipkartScraper: scrapers.NewFlipkartScraper(),
		chromeScraper:   browser.NewChromeScraper(),
		walmartScraper:  scrapers.NewWalmartScraper(),
		targetScraper:   scrapers.NewTargetScraper(),
		bestBuyScraper:  scrapers.NewBestBuyScraper(),
		cache:           cache.NewRedisCache(),
	}
}

func (s *SearchService) SearchProducts(params models.SearchParams) (*models.SearchResponse, error) {
	startTime := time.Now()

	// Set default country to IN (India) if not specified
	if params.Country == "" {
		params.Country = "IN"
	}

	// Validate input
	if err := s.validateSearchParams(&params); err != nil {
		return nil, err
	}

	// Try cache first
	cacheKey := ""
	if s.cache != nil && s.cache.IsAvailable() {
		cacheKey = s.cache.GenerateSearchKey(params)
		if cached, err := s.cache.GetSearchResults(cacheKey); err == nil && cached != nil {
			cached.Duration = fmt.Sprintf("%s (cached)", time.Since(startTime).String())
			log.Printf("Cache HIT for key: %s", cacheKey)
			return cached, nil
		}
		log.Printf("Cache MISS for key: %s", cacheKey)
	}

	// Cache miss or Redis unavailable - proceed with scraping
	country := strings.ToUpper(params.Country)

	allProducts := s.scrapeAllSources(params.Query, country)
	s.processProducts(allProducts)
	filteredProducts := s.applyFilters(allProducts, params.Filters)
	s.applySorting(filteredProducts, params.Sort)
	paginatedProducts, totalPages := s.applyPagination(filteredProducts, params.Page, params.Limit)

	duration := time.Since(startTime)

	// Update source information based on country
	sourceInfo := "Amazon, eBay"
	if country == "IN" {
		sourceInfo = "Amazon, eBay, Flipkart"
	}

	response := &models.SearchResponse{
		Query:      params.Query,
		Products:   paginatedProducts,
		Total:      len(filteredProducts),
		Page:       params.Page,
		Limit:      params.Limit,
		TotalPages: totalPages,
		Source:     sourceInfo,
		Filters:    params.Filters,
		Sort:       params.Sort,
		Duration:   duration.String(),
	}

	// Cache the response
	if s.cache != nil && s.cache.IsAvailable() && cacheKey != "" {
		if err := s.cache.SetSearchResults(cacheKey, response); err != nil {
			log.Printf("Failed to cache results: %v", err)
		} else {
			log.Printf("Cached results for key: %s", cacheKey)
		}
	}

	return response, nil
}

func (s *SearchService) scrapeAllSources(query, country string) []models.Product {
	var allProducts []models.Product
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Track errors for better debugging
	var scraperErrors []error
	var errorMu sync.Mutex

	// Helper function to safely append errors
	addError := func(err error) {
		if err != nil {
			errorMu.Lock()
			scraperErrors = append(scraperErrors, err)
			errorMu.Unlock()
		}
	}

	// Helper function to safely append products
	addProducts := func(products []models.Product, source string) {
		mu.Lock()
		allProducts = append(allProducts, products...)
		log.Printf("%s scraper completed: found %d products", source, len(products))
		mu.Unlock()
	}

	// Chrome universal scraping (disabled for now - uncomment when needed)
	// wg.Add(1)
	// go func() {
	//	defer wg.Done()
	//	chromeProducts, err := s.chromeScraper.SearchUniversal(query, country)
	//	addError(err)
	//	addProducts(chromeProducts, "Chrome")
	// }()

	// Amazon scraping
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Amazon scraper panic recovered: %v", r)
			}
		}()

		amazonProducts, err := s.amazonScraper.Search(query, country)
		addError(err)
		if amazonProducts == nil {
			amazonProducts = make([]models.Product, 0)
		}
		addProducts(amazonProducts, "Amazon")
	}()

	// eBay scraping
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("eBay scraper panic recovered: %v", r)
			}
		}()

		ebayProducts, err := s.ebayScraper.Search(query, country)
		addError(err)
		if ebayProducts == nil {
			ebayProducts = make([]models.Product, 0)
		}
		addProducts(ebayProducts, "eBay")
	}()

	// Flipkart scraping (only for India)
	if strings.ToUpper(country) == "IN" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Flipkart scraper panic recovered: %v", r)
				}
			}()

			flipkartProducts, err := s.flipkartScraper.Search(query, country)
			addError(err)
			if flipkartProducts == nil {
				flipkartProducts = make([]models.Product, 0)
			}
			addProducts(flipkartProducts, "Flipkart")
		}()
	}

	// Walmart scraping (only for US)
	if strings.ToUpper(country) == "US" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Walmart scraper panic recovered: %v", r)
				}
			}()

			walmartProducts, err := s.walmartScraper.Search(query, country)
			addError(err)
			if walmartProducts == nil {
				walmartProducts = make([]models.Product, 0)
			}
			addProducts(walmartProducts, "Walmart")
		}()
	}

	// Target scraping (only for US)
	if strings.ToUpper(country) == "US" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Target scraper panic recovered: %v", r)
				}
			}()

			targetProducts, err := s.targetScraper.Search(query, country)
			addError(err)
			if targetProducts == nil {
				targetProducts = make([]models.Product, 0)
			}
			addProducts(targetProducts, "Target")
		}()
	}

	// Best Buy scraping (only for US)
	if strings.ToUpper(country) == "US" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Best Buy scraper panic recovered: %v", r)
				}
			}()

			bestBuyProducts, err := s.bestBuyScraper.Search(query, country)
			addError(err)
			if bestBuyProducts == nil {
				bestBuyProducts = make([]models.Product, 0)
			}
			addProducts(bestBuyProducts, "Best Buy")
		}()
	}

	wg.Wait()

	// Log any errors that occurred
	if len(scraperErrors) > 0 {
		log.Printf("Scraping completed with %d errors:", len(scraperErrors))
		for i, err := range scraperErrors {
			log.Printf("  Error %d: %v", i+1, err)
		}
	}

	// Ensure we always return a valid slice
	if allProducts == nil {
		allProducts = make([]models.Product, 0)
	}

	log.Printf("Total products scraped: %d from %s", len(allProducts), country)
	return allProducts
}

func (s *SearchService) validateSearchParams(params *models.SearchParams) error {
	if params.Query == "" {
		return fmt.Errorf("search query cannot be empty")
	}

	// Set defaults
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Validate filters
	if params.Filters != nil {
		if params.Filters.MinPrice < 0 {
			return fmt.Errorf("minimum price cannot be negative")
		}
		if params.Filters.MaxPrice > 0 && params.Filters.MaxPrice < params.Filters.MinPrice {
			return fmt.Errorf("maximum price cannot be less than minimum price")
		}
		if params.Filters.MinRating < 0 || params.Filters.MinRating > 5 {
			return fmt.Errorf("minimum rating must be between 0 and 5")
		}
	}

	// Validate sort
	if params.Sort != nil {
		validFields := []string{"price", "rating", "name"}
		validOrders := []string{"asc", "desc"}

		if !contains(validFields, params.Sort.Field) {
			return fmt.Errorf("invalid sort field: %s. Valid fields: %s", params.Sort.Field, strings.Join(validFields, ", "))
		}
		if !contains(validOrders, params.Sort.Order) {
			return fmt.Errorf("invalid sort order: %s. Valid orders: %s", params.Sort.Order, strings.Join(validOrders, ", "))
		}
	}

	return nil
}

func (s *SearchService) processProducts(products []models.Product) {
	for i := range products {
		products[i].PriceValue = utils.ParsePrice(products[i].Price)
	}
}

func (s *SearchService) applyFilters(products []models.Product, filters *models.Filters) []models.Product {
	if filters == nil {
		return products
	}

	var filtered []models.Product

	for _, product := range products {
		// Price filter
		if filters.MinPrice > 0 && product.PriceValue < filters.MinPrice {
			continue
		}
		if filters.MaxPrice > 0 && product.PriceValue > filters.MaxPrice {
			continue
		}

		// Stock filter
		if filters.InStock != nil && product.InStock != *filters.InStock {
			continue
		}

		// Rating filter
		if filters.MinRating > 0 {
			rating := utils.ParseRating(product.Rating)
			if rating < filters.MinRating {
				continue
			}
		}

		// Source filter
		if filters.Source != "" {
			sourceMatch := false
			filterSource := strings.ToLower(filters.Source)
			productSource := strings.ToLower(product.Source)

			// Handle partial matches (e.g., "ebay" matches "eBay (Mock)")
			if strings.Contains(productSource, filterSource) || strings.Contains(filterSource, productSource) {
				sourceMatch = true
			}

			if !sourceMatch {
				continue
			}
		}

		filtered = append(filtered, product)
	}

	return filtered
}

func (s *SearchService) applySorting(products []models.Product, sortParams *models.Sort) {
	if sortParams == nil {
		return
	}

	sort.Slice(products, func(i, j int) bool {
		switch sortParams.Field {
		case "price":
			if sortParams.Order == "desc" {
				return products[i].PriceValue > products[j].PriceValue
			}
			return products[i].PriceValue < products[j].PriceValue

		case "rating":
			ratingI := utils.ParseRating(products[i].Rating)
			ratingJ := utils.ParseRating(products[j].Rating)
			if sortParams.Order == "desc" {
				return ratingI > ratingJ
			}
			return ratingI < ratingJ

		case "name":
			if sortParams.Order == "desc" {
				return products[i].Name > products[j].Name
			}
			return products[i].Name < products[j].Name

		default:
			return false
		}
	})
}

func (s *SearchService) applyPagination(products []models.Product, page, limit int) ([]models.Product, int) {
	total := len(products)
	totalPages := int(math.Ceil(float64(total) / float64(limit)))

	start := (page - 1) * limit
	if start >= total {
		return []models.Product{}, totalPages
	}

	end := start + limit
	if end > total {
		end = total
	}

	return products[start:end], totalPages
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
