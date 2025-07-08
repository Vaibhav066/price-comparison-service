package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/time/rate"
	"price-comparison-api/internal/models"
	"price-comparison-api/internal/scrapers"
	"price-comparison-api/internal/services"
	"price-comparison-api/pkg/browser"
	"price-comparison-api/pkg/cache"
)

var (
	rateLimiters = make(map[string]*rate.Limiter)
	rateMutex    = &sync.RWMutex{}
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	searchService := services.NewSearchService()
	redisCache := cache.NewRedisCache()

	r := gin.Default()

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Add request ID middleware
	r.Use(func(c *gin.Context) {
		requestID := fmt.Sprintf("%d", time.Now().UnixNano())
		c.Header("X-Request-ID", requestID)
		start := time.Now()
		c.Next()
		log.Printf("[%s] %s %s - %v - %d",
			requestID, c.Request.Method, c.Request.URL.Path,
			time.Since(start), c.Writer.Status())
	})

	// Add rate limiting middleware (ADD THIS)
	r.Use(rateLimitMiddleware())

	// Enhanced health check with cache status
	r.GET("/health", func(c *gin.Context) {
		health := gin.H{
			"status":  "healthy",
			"service": "price-comparison-api",
			"version": "1.0.0",
		}

		if redisCache != nil && redisCache.IsAvailable() {
			health["cache"] = "redis connected"
		} else {
			health["cache"] = "redis unavailable"
		}

		c.JSON(http.StatusOK, health)
	})

	// Rate limit status endpoint
	r.GET("/rate-limit/status", func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getRateLimiter(ip)

		c.JSON(http.StatusOK, gin.H{
			"ip":               ip,
			"limit_per_second": limiter.Limit(),
			"burst_capacity":   limiter.Burst(),
			"tokens_available": limiter.Tokens(),
			"next_token_at":    time.Now().Add(time.Second / time.Duration(limiter.Limit())),
		})
	})

	// Cache stats endpoint
	r.GET("/cache/stats", func(c *gin.Context) {
		if redisCache == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "cache not available",
			})
			return
		}

		stats := redisCache.GetStats()
		c.JSON(http.StatusOK, stats)
	})

	// Cache debug endpoint
	r.GET("/cache/debug", func(c *gin.Context) {
		if redisCache == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "cache not available",
			})
			return
		}

		keys := redisCache.GetAllKeys()

		// Get detailed info for each key
		keyDetails := make([]gin.H, 0, len(keys))
		for _, key := range keys {
			ttl := redisCache.GetKeyTTL(key)
			keyDetails = append(keyDetails, gin.H{
				"key":         key,
				"ttl_seconds": int(ttl.Seconds()),
				"expires_in":  ttl.String(),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"total_keys":  len(keys),
			"cache_keys":  keyDetails,
			"cache_stats": redisCache.GetStats(),
			"debug_info": gin.H{
				"redis_available": redisCache.IsAvailable(),
				"timestamp":       time.Now().Format(time.RFC3339),
			},
		})
	})

	// Cache flush endpoint (for testing)
	r.DELETE("/cache/flush", func(c *gin.Context) {
		if redisCache == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "cache not available",
			})
			return
		}

		if err := redisCache.FlushCache(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to flush cache",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "cache flushed successfully",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// Enhanced search endpoint with caching
	r.GET("/search", func(c *gin.Context) {
		params := parseSearchParams(c)

		results, err := searchService.SearchProducts(params)
		if err != nil {
			log.Printf("Search error: %v", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:   "search_failed",
				Code:    http.StatusBadRequest,
				Message: err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, results)
	})

	// Test Chrome availability
	r.GET("/test/chrome-basic", func(c *gin.Context) {
		log.Printf("Testing basic Chrome functionality...")

		// Enhanced Chrome options for macOS
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.Flag("disable-extensions", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-web-security", true),
			chromedp.Flag("disable-features", "VizDisplayCompositor"),
			chromedp.Flag("disable-background-timer-throttling", true),
			chromedp.Flag("disable-backgrounding-occluded-windows", true),
			chromedp.Flag("disable-renderer-backgrounding", true),
			chromedp.Flag("disable-field-trial-config", true),
			chromedp.Flag("disable-ipc-flooding-protection", true),
			chromedp.ExecPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"),
		)

		allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
		defer allocCancel()

		ctx, cancel := chromedp.NewContext(allocCtx)
		defer cancel()

		ctx, timeoutCancel := context.WithTimeout(ctx, 15*time.Second)
		defer timeoutCancel()

		var title string
		err := chromedp.Run(ctx,
			chromedp.Navigate("https://httpbin.org/get"),
			chromedp.Sleep(2*time.Second),
			chromedp.WaitVisible("body", chromedp.ByQuery),
			chromedp.Title(&title),
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "Chrome test failed",
				"details":    err.Error(),
				"suggestion": "Try installing Chrome: brew install --cask google-chrome",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "Chrome working",
			"title":   title,
			"message": "Chrome browser is properly configured",
		})
	})

	// Test Chrome scraper individually
	r.GET("/test/chrome", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "US"
		}

		chromeScraper := browser.NewChromeScraper()
		defer chromeScraper.Close()

		products, err := chromeScraper.SearchUniversal(query, country)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Chrome scraper failed",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Chrome",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
		})
	})

	// Test Amazon scraper individually
	r.GET("/test/amazon", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "IN"
		}

		amazonScraper := scrapers.NewAmazonScraper()
		products, err := amazonScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Amazon",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// Test eBay scraper individually
	r.GET("/test/ebay", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "IN"
		}

		ebayScraper := scrapers.NewEbayScraper()
		products, err := ebayScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "eBay",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// Test Flipkart scraper individually
	r.GET("/test/flipkart", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "IN"
		}

		flipkartScraper := scrapers.NewFlipkartScraper()
		products, err := flipkartScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Flipkart",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// Test Walmart scraper individually
	r.GET("/test/walmart", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "US"
		}

		walmartScraper := scrapers.NewWalmartScraper()
		products, err := walmartScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Walmart",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// Test Target scraper individually
	r.GET("/test/target", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "US"
		}

		targetScraper := scrapers.NewTargetScraper()
		products, err := targetScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Target",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// Test Best Buy scraper individually
	r.GET("/test/bestbuy", func(c *gin.Context) {
		query := c.Query("q")
		country := c.Query("country")
		if query == "" {
			query = "smartphone"
		}
		if country == "" {
			country = "US"
		}

		bestBuyScraper := scrapers.NewBestBuyScraper()
		products, err := bestBuyScraper.Search(query, country)

		c.JSON(http.StatusOK, gin.H{
			"scraper":  "Best Buy",
			"country":  country,
			"query":    query,
			"count":    len(products),
			"products": products,
			"error":    err,
		})
	})

	// API info endpoint
	r.GET("/api/info", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":        "Price Comparison API",
			"version":     "1.0.0",
			"description": "API for comparing product prices across multiple sources",
			"features":    []string{"Multi-source scraping", "Price comparison", "Redis caching", "Filtering", "Sorting", "Pagination"},
			"endpoints": map[string]string{
				"GET /search":      "Search products with filtering and sorting",
				"GET /health":      "Health check",
				"GET /cache/stats": "Cache statistics",
				"GET /api/info":    "API information",
			},
			"supported_sources": []string{"Amazon", "eBay"},
		})
	})

	log.Printf("Starting cached server on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func parseSearchParams(c *gin.Context) models.SearchParams {
	query := c.Query("q")
	country := c.Query("country")

	page := 1
	if p := c.Query("page"); p != "" {
		if pageNum, err := strconv.Atoi(p); err == nil && pageNum > 0 {
			page = pageNum
		}
	}

	limit := 10
	if l := c.Query("limit"); l != "" {
		if limitNum, err := strconv.Atoi(l); err == nil && limitNum > 0 {
			limit = limitNum
		}
	}

	// Parse filters
	var filters *models.Filters
	if minPrice := c.Query("min_price"); minPrice != "" {
		if filters == nil {
			filters = &models.Filters{}
		}
		if price, err := strconv.ParseFloat(minPrice, 64); err == nil {
			filters.MinPrice = price
		}
	}

	if maxPrice := c.Query("max_price"); maxPrice != "" {
		if filters == nil {
			filters = &models.Filters{}
		}
		if price, err := strconv.ParseFloat(maxPrice, 64); err == nil {
			filters.MaxPrice = price
		}
	}

	if source := c.Query("source"); source != "" {
		if filters == nil {
			filters = &models.Filters{}
		}
		filters.Source = source
	}

	if inStock := c.Query("in_stock"); inStock != "" {
		if filters == nil {
			filters = &models.Filters{}
		}
		if stock, err := strconv.ParseBool(inStock); err == nil {
			filters.InStock = &stock
		}
	}

	if minRating := c.Query("min_rating"); minRating != "" {
		if filters == nil {
			filters = &models.Filters{}
		}
		if rating, err := strconv.ParseFloat(minRating, 64); err == nil {
			filters.MinRating = rating
		}
	}

	// Parse sort
	var sort *models.Sort
	if sortField := c.Query("sort"); sortField != "" {
		sort = &models.Sort{
			Field: sortField,
			Order: "asc", // default
		}
		if sortOrder := c.Query("order"); sortOrder != "" {
			sort.Order = sortOrder
		}
	}

	return models.SearchParams{
		Query:   query,
		Country: country,
		Page:    page,
		Limit:   limit,
		Filters: filters,
		Sort:    sort,
	}
}

func getRateLimiter(ip string) *rate.Limiter {
	rateMutex.RLock()
	limiter, exists := rateLimiters[ip]
	rateMutex.RUnlock()

	if !exists {
		rateMutex.Lock()
		limiter = rate.NewLimiter(rate.Limit(10), 20) // 10 req/sec, burst 20
		rateLimiters[ip] = limiter
		rateMutex.Unlock()
	}

	return limiter
}

func rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getRateLimiter(ip)

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate_limit_exceeded",
				"message":     "Too many requests from your IP",
				"retry_after": "1 second",
				"ip":          ip,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
