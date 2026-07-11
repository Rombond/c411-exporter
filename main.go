package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

type config struct {
	BaseURL        string
	Port           string
	MetricsPath    string
	ScrapeInterval time.Duration
	Username       string
	Password       string
	LoginURL       string
	UsersURL       string
}

func loadConfig() config {
	base := "https://" + getEnvOrDefault("C411_API_BASE_URL", "c411.org")
	return config{
		BaseURL:        base,
		Port:           getEnvOrDefault("PORT", "9090"),
		MetricsPath:    getEnvOrDefault("METRICS_PATH", "/metrics"),
		Username:       os.Getenv("C411_USERNAME"),
		Password:       os.Getenv("C411_PASSWORD"),
		ScrapeInterval: parseDuration(os.Getenv("SCRAPE_INTERVAL"), 5*time.Minute),
		// Use web login URL for proper auth flow - visits /login first to get session cookies
		LoginURL:       base + "/login",
		UsersURL:       base + "/api/auth/me",
	}
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// -----------------------------------------------------------------------------
// API client
// -----------------------------------------------------------------------------

// C411Client manages authentication and communication with the C411 API.
type C411Client struct {
	mu          sync.RWMutex
	cookies     []*http.Cookie
	csrfToken   string
	baseURL     string
	loginURL    string
	apiLoginURL string
	usersURL    string
	httpClient  *http.Client
}

// c411Jar implements http.CookieJar with per-URL cookie storage
type c411Jar struct {
	cookies map[string][]*http.Cookie // keyed by "scheme://host"
}

// Cookies returns the cookie jar's cookies for the given URL.
func (j *c411Jar) Cookies(u *url.URL) []*http.Cookie {
	key := u.Scheme + "://" + u.Host
	if cookies, ok := j.cookies[key]; ok {
		return cookies
	}
	return nil
}

// SetCookies stores cookies for the given URL.
func (j *c411Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	key := u.Scheme + "://" + u.Host
	if j.cookies == nil {
		j.cookies = make(map[string][]*http.Cookie)
	}
	// Merge or replace cookies for this URL
	j.cookies[key] = append(j.cookies[key], cookies...)
}

func newC411Client(baseURL, loginURL, usersURL string) *C411Client {
	jar := &c411Jar{cookies: make(map[string][]*http.Cookie)}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	return &C411Client{
		baseURL:     baseURL,
		loginURL:    loginURL,
		apiLoginURL: baseURL + "/api/auth/login",
		usersURL:    usersURL,
		httpClient:  client,
	}
}

// FirstRequest fetches the CSRF token from the login page.
func (c *C411Client) FirstRequest() error {
	fmt.Printf("[auth] Fetching login page: %s\n", c.loginURL)

	req, err := http.NewRequest(http.MethodGet, c.loginURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build first request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", c.baseURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("first request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	html := string(body)
	fmt.Printf("[auth] Response status: %s, cookies: %+v\n", resp.Status, resp.Cookies())

	// Extract CSRF token from <meta name="csrf-token" content="...">
	re := regexp.MustCompile(`(?i)<meta[^>]*name=["']csrf-token["'][^>]*content=["']([^"']+)["']`)
	matches := re.FindStringSubmatch(html)
	if len(matches) >= 2 {
		c.mu.Lock()
		c.cookies = resp.Cookies()
		c.csrfToken = matches[1]
		c.mu.Unlock()

		fmt.Printf("[auth] CSRF token extracted, cookies stored: %+v\n", c.cookies)
		return nil
	}

	return fmt.Errorf("failed to extract CSRF token from login page")
}

func (c *C411Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cookies) > 0
}

// Login authenticates with the C411 API and stores the returned session cookies.
func (c *C411Client) Login(username, password string) error {
	if err := c.FirstRequest(); err != nil {
		return fmt.Errorf("first request failed: %w", err)
	}

	// Build JSON body (matching the real JS client)
	payload, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return fmt.Errorf("failed to encode login payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.apiLoginURL, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("failed to build login request: %w", err)
	}

	// Attach cookies from FirstRequest (includes __csrf)
	c.mu.RLock()
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	c.mu.RUnlock()

	// Headers matching the real JS client
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.loginURL)
	req.Header.Set("csrf-token", c.csrfToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[auth] Login failed: %s\n%s\n", resp.Status, string(respBody))
		return fmt.Errorf("login failed: %s", resp.Status)
	}

	// Merge login response cookies (session) with existing cookies (__csrf)
	c.mu.Lock()
	c.cookies = append(c.cookies, resp.Cookies()...)
	c.mu.Unlock()

	fmt.Printf("[auth] Authenticated as %s, cookies: %+v\n", username, c.cookies)
	return nil
}

// FetchMetrics retrieves the current user's metrics from the C411 API.
func (c *C411Client) FetchMetrics() (*UserMetrics, error) {
	c.mu.RLock()
	cookies := c.cookies
	c.mu.RUnlock()

	if len(cookies) == 0 {
		return nil, fmt.Errorf("not authenticated")
	}

	req, err := http.NewRequest(http.MethodGet, c.usersURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build metrics request: %w", err)
	}

	// Attach stored cookies to the request
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("authentication required")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected metrics response: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics response: %w", err)
	}

	var wrapper userWrapper
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse metrics response: %w", err)
	}

	return &UserMetrics{
		TotalUploadedBytes:   wrapper.User.Uploaded,
		TotalDownloadedBytes: wrapper.User.Downloaded,
		Username:             wrapper.User.Username,
	}, nil
}

// -----------------------------------------------------------------------------
// Domain types
// -----------------------------------------------------------------------------

type userWrapper struct {
	User struct {
		Uploaded   int64  `json:"uploaded"`
		Downloaded int64  `json:"downloaded"`
		Username   string `json:"username"`
	} `json:"user"`
}

type UserMetrics struct {
	TotalUploadedBytes   int64
	TotalDownloadedBytes int64
	Username             string
}

// -----------------------------------------------------------------------------
// Prometheus metrics
// -----------------------------------------------------------------------------

type exporterMetrics struct {
	totalUploaded   prometheus.Gauge
	totalDownloaded prometheus.Gauge
}

func newExporterMetrics(reg prometheus.Registerer) *exporterMetrics {
	m := &exporterMetrics{
		totalUploaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "c411",
			Name:      "total_uploaded_bytes",
			Help:      "Total uploaded bytes from C411 API",
		}),
		totalDownloaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "c411",
			Name:      "total_downloaded_bytes",
			Help:      "Total downloaded bytes from C411 API",
		}),
	}
	reg.MustRegister(m.totalUploaded, m.totalDownloaded)
	return m
}

func (m *exporterMetrics) update(u *UserMetrics) {
	m.totalUploaded.Set(float64(u.TotalUploadedBytes))
	m.totalDownloaded.Set(float64(u.TotalDownloadedBytes))
}

// -----------------------------------------------------------------------------
// HTTP handlers
// -----------------------------------------------------------------------------

type server struct {
	mu          sync.RWMutex
	client      *C411Client
	metrics     *exporterMetrics
	lastMetrics *UserMetrics
	username    string
	password    string
}

func (s *server) metricsHandler(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4")
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}

// scrapeAndRefresh fetches metrics from C411 and updates the Prometheus gauges.
// If the fetch fails it attempts to reconnect and retry once before giving up.
func (s *server) scrapeAndRefresh() {
	// --- first attempt ---------------------------------------------------
	if !s.client.IsAuthenticated() {
		fmt.Println("[scraper] Not authenticated, attempting reconnect...")
	} else {
		userMetrics, err := s.client.FetchMetrics()
		if err == nil {
			s.applyMetrics(userMetrics)
			return
		}
		fmt.Printf("[scraper] Fetch failed: %v, attempting reconnect...\n", err)
	}

	// --- reconnect and retry once ----------------------------------------
	if err := s.reconnect(); err != nil {
		fmt.Printf("[scraper] Reconnect failed: %v\n", err)
		return
	}

	userMetrics, err := s.client.FetchMetrics()
	if err != nil {
		fmt.Printf("[scraper] Fetch still failing after reconnect: %v\n", err)
		return
	}

	s.applyMetrics(userMetrics)
}

// reconnect re-authenticates using the stored credentials.
func (s *server) reconnect() error {
	if s.username == "" || s.password == "" {
		return fmt.Errorf("cannot reconnect: no credentials configured")
	}
	fmt.Println("[scraper] Reconnecting...")
	return s.client.Login(s.username, s.password)
}

// applyMetrics updates the Prometheus gauges and stores the latest snapshot.
func (s *server) applyMetrics(userMetrics *UserMetrics) {
	s.metrics.update(userMetrics)
	s.mu.Lock()
	s.lastMetrics = userMetrics
	s.mu.Unlock()

	fmt.Printf("[scraper] Updated metrics: %s (↑%d ↓%d)\n",
		userMetrics.Username, userMetrics.TotalUploadedBytes, userMetrics.TotalDownloadedBytes)
}

// startMetricsRefresher launches a background goroutine that periodically
// scrapes C411 data and updates the Prometheus gauges.
func (s *server) startMetricsRefresher(interval time.Duration) {
	// Perform an initial scrape immediately
	go s.scrapeAndRefresh()

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			s.scrapeAndRefresh()
		}
	}()
}

func (s *server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":        "healthy",
		"authenticated": s.client.IsAuthenticated(),
	})
}

// -----------------------------------------------------------------------------
// Entrypoint
// -----------------------------------------------------------------------------

func main() {
	cfg := loadConfig()

	client := newC411Client(cfg.BaseURL, cfg.LoginURL, cfg.UsersURL)
	metrics := newExporterMetrics(prometheus.DefaultRegisterer)
	srv := &server{client: client, metrics: metrics, username: cfg.Username, password: cfg.Password}

	// Attempt auto-login on startup if credentials are provided.
	if cfg.Username != "" && cfg.Password != "" {
		fmt.Println("[auth] Credentials found, attempting auto-login...")
		if err := client.Login(cfg.Username, cfg.Password); err != nil {
			fmt.Printf("[auth] Auto-login failed (non-fatal): %v\n", err)
		}
	} else {
		fmt.Println("[auth] No credentials provided, auto-login skipped")
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET(cfg.MetricsPath, srv.metricsHandler)
	r.GET("/health", srv.healthHandler)

			// Start background metrics refresher
	srv.startMetricsRefresher(cfg.ScrapeInterval)
	fmt.Printf("[server] Metrics refresh interval: %v\n", cfg.ScrapeInterval)

	addr := ":" + cfg.Port
	fmt.Printf("[server] Starting C411 exporter on %s\n", addr)
	fmt.Printf("[server] Metrics available at: http://localhost%s%s\n", addr, cfg.MetricsPath)

	if err := r.Run(addr); err != nil {
		fmt.Printf("[server] Error starting server: %v\n", err)
		os.Exit(1)
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// parseDuration parses a simple duration string (e.g. "30s", "5m", "2h").
// Returns fallback if the string is empty or cannot be parsed.
func parseDuration(s string, fallback time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	units := []struct {
		suffix string
		mult   time.Duration
	}{
		{"h", time.Hour},
		{"m", time.Minute},
		{"s", time.Second},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			var n int
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > 0 {
				return time.Duration(n) * u.mult
			}
		}
	}
	return fallback
}
