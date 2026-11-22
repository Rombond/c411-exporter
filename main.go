package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const minScrapeInterval = 30 * time.Second // the API refreshes data at most every 30 s

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

type config struct {
	BaseURL        string
	APIKey         string
	Port           string
	MetricsPath    string
	ScrapeInterval time.Duration
}

func loadConfig() config {
	base := getEnvOrDefault("C411_API_BASE_URL", "c411.org")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	interval, err := time.ParseDuration(getEnvOrDefault("SCRAPE_INTERVAL", "5m"))
	if err != nil {
		log.Printf("[config] invalid SCRAPE_INTERVAL, using 5m: %v", err)
		interval = 5 * time.Minute
	}
	if interval < minScrapeInterval {
		log.Printf("[config] SCRAPE_INTERVAL %v below %v, raised", interval, minScrapeInterval)
		interval = minScrapeInterval
	}
	return config{
		BaseURL:        strings.TrimRight(base, "/"),
		APIKey:         strings.TrimSpace(os.Getenv("C411_API_KEY")),
		Port:           getEnvOrDefault("PORT", "9090"),
		MetricsPath:    getEnvOrDefault("METRICS_PATH", "/metrics"),
		ScrapeInterval: interval,
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

type transferAmount struct {
	Actual int64 `json:"actual"`
	Credit int64 `json:"credit"`
	Total  int64 `json:"total"`
}

type voie struct {
	Star            bool `json:"star"`
	QuestsCompleted int  `json:"questsCompleted"`
	QuestsTotal     int  `json:"questsTotal"`
}

// Profile is the response of GET /api/user/profile (scope profile:read).
type Profile struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	MemberSince string `json:"memberSince"`
	Level       struct {
		Level         int   `json:"level"`
		XP            int64 `json:"xp"`
		XPIntoLevel   int64 `json:"xpIntoLevel"`
		XPToNextLevel int64 `json:"xpToNextLevel"`
	} `json:"level"`
	Class struct {
		Slug          string `json:"slug"`
		EffectiveSlug string `json:"effectiveSlug"`
		Foundation    string `json:"foundation"`
		TreeBonusBp   int    `json:"treeBonusBp"`
		Progress      struct {
			QuestsCompleted int `json:"questsCompleted"`
			QuestsTotal     int `json:"questsTotal"`
			Stars           int `json:"stars"`
			Grade           struct {
				Tier  string `json:"tier"`
				Level int    `json:"level"`
			} `json:"grade"`
		} `json:"progress"`
		Voies map[string]voie `json:"voies"`
	} `json:"class"`
	Team *struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Tag  string `json:"tag"`
		Role string `json:"role"`
	} `json:"team"`
	Zen struct {
		Balance     int64 `json:"balance"`
		TotalEarned int64 `json:"totalEarned"`
		TotalSpent  int64 `json:"totalSpent"`
	} `json:"zen"`
	Transfer struct {
		Uploaded   transferAmount `json:"uploaded"`
		Downloaded transferAmount `json:"downloaded"`
	} `json:"transfer"`
	Download struct {
		Allowed       bool    `json:"allowed"`
		BlockedReason *string `json:"blockedReason"`
	} `json:"download"`
	Freeleech struct {
		Active  bool     `json:"active"`
		Sources []string `json:"sources"`
	} `json:"freeleech"`
	SilverLeech  bool              `json:"silverLeech"`
	LeechBonuses []json.RawMessage `json:"leechBonuses"`
}

type apiClient struct {
	profileURL string
	apiKey     string
	http       *http.Client
}

func newAPIClient(baseURL, apiKey string) *apiClient {
	return &apiClient{
		profileURL: baseURL + "/api/user/profile",
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *apiClient) FetchProfile() (*Profile, error) {
	req, err := http.NewRequest(http.MethodGet, c.profileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "c411-exporter/2")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, errors.New("401: API key missing or invalid")
	case http.StatusForbidden:
		return nil, errors.New("403: API key lacks the profile:read scope (create a new key with it)")
	case http.StatusTooManyRequests:
		return nil, errors.New("429: rate limit reached, raise SCRAPE_INTERVAL")
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(body))
	}

	var p Profile
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	return &p, nil
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// -----------------------------------------------------------------------------
// Prometheus metrics
// -----------------------------------------------------------------------------

const ns = "c411"

type exporterMetrics struct {
	// v1 names kept for dashboard compatibility (= transfer.*.total)
	totalUploaded   prometheus.Gauge
	totalDownloaded prometheus.Gauge

	uploaded       *prometheus.GaugeVec
	downloaded     *prometheus.GaugeVec
	ratio          prometheus.Gauge
	level          prometheus.Gauge
	xp             prometheus.Gauge
	xpIntoLevel    prometheus.Gauge
	xpToNextLevel  prometheus.Gauge
	zenBalance     prometheus.Gauge
	zenEarned      prometheus.Gauge
	zenSpent       prometheus.Gauge
	quests         *prometheus.GaugeVec
	stars          prometheus.Gauge
	gradeLevel     prometheus.Gauge
	treeBonusBp    prometheus.Gauge
	voieQuests     *prometheus.GaugeVec
	voieStar       *prometheus.GaugeVec
	downloadAllow  prometheus.Gauge
	freeleech      prometheus.Gauge
	silverLeech    prometheus.Gauge
	leechBonuses   prometheus.Gauge
	userInfo       *prometheus.GaugeVec
	up             prometheus.Gauge
	lastSuccess    prometheus.Gauge
	scrapeFailures prometheus.Counter
}

func gauge(name, help string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{Namespace: ns, Name: name, Help: help})
}

func gaugeVec(name, help string, labels ...string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: ns, Name: name, Help: help}, labels)
}

func newExporterMetrics(reg prometheus.Registerer) *exporterMetrics {
	m := &exporterMetrics{
		totalUploaded:   gauge("total_uploaded_bytes", "Total uploaded bytes, real + credit (same as c411_uploaded_bytes{kind=\"total\"})."),
		totalDownloaded: gauge("total_downloaded_bytes", "Total downloaded bytes, real + credit (same as c411_downloaded_bytes{kind=\"total\"})."),
		uploaded:        gaugeVec("uploaded_bytes", "Uploaded bytes by kind (actual, credit, total).", "kind"),
		downloaded:      gaugeVec("downloaded_bytes", "Downloaded bytes by kind (actual, credit, total).", "kind"),
		ratio:           gauge("ratio", "Total uploaded / total downloaded (0 when nothing downloaded)."),
		level:           gauge("level", "User level."),
		xp:              gauge("xp", "Total XP."),
		xpIntoLevel:     gauge("xp_into_level", "XP earned in the current level."),
		xpToNextLevel:   gauge("xp_to_next_level", "XP still needed for the next level."),
		zenBalance:      gauge("zen_balance", "Current zen balance."),
		zenEarned:       gauge("zen_earned_total", "Zen earned since account creation."),
		zenSpent:        gauge("zen_spent_total", "Zen spent since account creation."),
		quests:          gaugeVec("class_quests", "Class quests, state=completed|total.", "state"),
		stars:           gauge("class_stars", "Class stars."),
		gradeLevel:      gauge("class_grade_level", "Level inside the current grade tier."),
		treeBonusBp:     gauge("class_tree_bonus_basis_points", "Class tree bonus in basis points."),
		voieQuests:      gaugeVec("voie_quests", "Quests per voie, state=completed|total.", "voie", "state"),
		voieStar:        gaugeVec("voie_star", "1 if the voie star is earned.", "voie"),
		downloadAllow:   gauge("download_allowed", "1 if downloading is allowed."),
		freeleech:       gauge("freeleech_active", "1 if freeleech is active."),
		silverLeech:     gauge("silverleech_active", "1 if silverleech is active."),
		leechBonuses:    gauge("leech_bonuses", "Number of active leech bonuses."),
		userInfo:        gaugeVec("user_info", "User info, value is always 1.", "username", "class", "grade_tier", "team"),
		up:              gauge("up", "1 if the last API call succeeded."),
		lastSuccess:     gauge("last_success_timestamp_seconds", "Unix time of the last successful API call."),
		scrapeFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "scrape_failures_total", Help: "Failed API calls.",
		}),
	}
	reg.MustRegister(
		m.totalUploaded, m.totalDownloaded, m.uploaded, m.downloaded, m.ratio,
		m.level, m.xp, m.xpIntoLevel, m.xpToNextLevel,
		m.zenBalance, m.zenEarned, m.zenSpent,
		m.quests, m.stars, m.gradeLevel, m.treeBonusBp, m.voieQuests, m.voieStar,
		m.downloadAllow, m.freeleech, m.silverLeech, m.leechBonuses,
		m.userInfo, m.up, m.lastSuccess, m.scrapeFailures,
	)
	return m
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func setTransfer(v *prometheus.GaugeVec, t transferAmount) {
	v.WithLabelValues("actual").Set(float64(t.Actual))
	v.WithLabelValues("credit").Set(float64(t.Credit))
	v.WithLabelValues("total").Set(float64(t.Total))
}

func (m *exporterMetrics) update(p *Profile) {
	up, down := p.Transfer.Uploaded, p.Transfer.Downloaded
	m.totalUploaded.Set(float64(up.Total))
	m.totalDownloaded.Set(float64(down.Total))
	setTransfer(m.uploaded, up)
	setTransfer(m.downloaded, down)
	if down.Total > 0 {
		m.ratio.Set(float64(up.Total) / float64(down.Total))
	} else {
		m.ratio.Set(0)
	}

	m.level.Set(float64(p.Level.Level))
	m.xp.Set(float64(p.Level.XP))
	m.xpIntoLevel.Set(float64(p.Level.XPIntoLevel))
	m.xpToNextLevel.Set(float64(p.Level.XPToNextLevel))

	m.zenBalance.Set(float64(p.Zen.Balance))
	m.zenEarned.Set(float64(p.Zen.TotalEarned))
	m.zenSpent.Set(float64(p.Zen.TotalSpent))

	prog := p.Class.Progress
	m.quests.WithLabelValues("completed").Set(float64(prog.QuestsCompleted))
	m.quests.WithLabelValues("total").Set(float64(prog.QuestsTotal))
	m.stars.Set(float64(prog.Stars))
	m.gradeLevel.Set(float64(prog.Grade.Level))
	m.treeBonusBp.Set(float64(p.Class.TreeBonusBp))
	m.voieQuests.Reset()
	m.voieStar.Reset()
	for name, v := range p.Class.Voies {
		m.voieQuests.WithLabelValues(name, "completed").Set(float64(v.QuestsCompleted))
		m.voieQuests.WithLabelValues(name, "total").Set(float64(v.QuestsTotal))
		m.voieStar.WithLabelValues(name).Set(b2f(v.Star))
	}

	m.downloadAllow.Set(b2f(p.Download.Allowed))
	m.freeleech.Set(b2f(p.Freeleech.Active))
	m.silverLeech.Set(b2f(p.SilverLeech))
	m.leechBonuses.Set(float64(len(p.LeechBonuses)))

	team := ""
	if p.Team != nil {
		team = p.Team.Name
	}
	m.userInfo.Reset() // labels can change (class, grade, team)
	m.userInfo.WithLabelValues(p.Username, p.Class.EffectiveSlug, prog.Grade.Tier, team).Set(1)

	m.up.Set(1)
	m.lastSuccess.Set(float64(time.Now().Unix()))
}

// -----------------------------------------------------------------------------
// Server
// -----------------------------------------------------------------------------

type server struct {
	client  *apiClient
	metrics *exporterMetrics

	mu          sync.RWMutex
	lastSuccess time.Time
	lastError   string
}

// scrape calls the API once and updates the gauges. On failure the previous
// values are kept and c411_up drops to 0.
func (s *server) scrape() {
	p, err := s.client.FetchProfile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastError = err.Error()
		s.metrics.up.Set(0)
		s.metrics.scrapeFailures.Inc()
		log.Printf("[scraper] fetch failed: %v", err)
		return
	}
	s.metrics.update(p)
	s.lastSuccess = time.Now()
	s.lastError = ""
	log.Printf("[scraper] updated %s (up %d, down %d, level %d)",
		p.Username, p.Transfer.Uploaded.Total, p.Transfer.Downloaded.Total, p.Level.Level)
}

func (s *server) run(interval time.Duration) {
	s.scrape()
	for range time.Tick(interval) {
		s.scrape()
	}
}

func (s *server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resp := map[string]any{"status": "healthy", "last_error": s.lastError}
	if !s.lastSuccess.IsZero() {
		resp["last_success"] = s.lastSuccess.UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	cfg := loadConfig()
	if cfg.APIKey == "" {
		log.Fatal("[config] C411_API_KEY is required (create a key with the profile:read scope)")
	}

	srv := &server{
		client:  newAPIClient(cfg.BaseURL, cfg.APIKey),
		metrics: newExporterMetrics(prometheus.DefaultRegisterer),
	}
	go srv.run(cfg.ScrapeInterval)

	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.Handler())
	mux.HandleFunc("/health", srv.healthHandler)

	addr := ":" + cfg.Port
	log.Printf("[server] c411 exporter v2 on %s%s, refresh every %v", addr, cfg.MetricsPath, cfg.ScrapeInterval)
	log.Fatal(http.ListenAndServe(addr, mux))
}
