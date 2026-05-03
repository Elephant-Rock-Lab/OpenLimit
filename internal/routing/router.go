package routing

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"openlimit/internal/config"
	"openlimit/internal/providers"
)

type Plan struct {
	Targets []providers.Target
}

// LatencyReader provides per-region latency data for routing decisions.
type LatencyReader interface {
	RegionLatency(provider, model, region string) (p50 time.Duration, ok bool)
}

// HealthChecker reports whether a provider:region:model triple is healthy.
// Implemented by health.Tracker; defined here to avoid circular imports.
type HealthChecker interface {
	IsHealthy(provider, model, region string) bool
}

// Router selects provider targets for model requests with optional region awareness.
type Router struct {
	models          map[string]config.ModelConfig
	providerRegions map[string][]config.RegionConfig // provider → regions
	regionByBaseURL map[string]string                // normalized base URL → region name
	regionDataRes   map[string]string                // region name → data residency tag
	defaultRegion   string                           // gateway's own region
	strategy        string                           // "priority" or "latency"
	latencyCache    *LatencyCache
	health          HealthChecker // nil = no health tracking
	rng             *rand.Rand
}

// LatencyCache caches per-region p50 latencies to avoid per-request histogram scans.
type LatencyCache struct {
	mu      sync.RWMutex
	entries map[string]time.Duration // "provider:model:region" → p50
	updated time.Time
	ttl     time.Duration
	reader  LatencyReader
	// knownCombos is populated at construction time with all (provider, model, region) triples.
	knownCombos []combo
}

type combo struct {
	provider string
	model    string
	region   string
}

func NewLatencyCache(reader LatencyReader, combos []combo, ttl time.Duration) *LatencyCache {
	return &LatencyCache{
		entries:     make(map[string]time.Duration),
		ttl:         ttl,
		reader:      reader,
		knownCombos: combos,
	}
}

func (c *LatencyCache) Get(provider, model, region string) (time.Duration, bool) {
	c.mu.RLock()
	if time.Since(c.updated) <= c.ttl {
		d, ok := c.entries[provider+":"+model+":"+region]
		c.mu.RUnlock()
		return d, ok
	}
	c.mu.RUnlock()

	// Refresh needed
	c.Refresh()

	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.entries[provider+":"+model+":"+region]
	return d, ok
}

func (c *LatencyCache) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(c.updated) <= c.ttl {
		return
	}

	newEntries := make(map[string]time.Duration, len(c.knownCombos))
	for _, cb := range c.knownCombos {
		if p50, ok := c.reader.RegionLatency(cb.provider, cb.model, cb.region); ok {
			newEntries[cb.provider+":"+cb.model+":"+cb.region] = p50
		}
	}
	c.entries = newEntries
	c.updated = time.Now()
}

// New creates a Router from model and provider configs.
func New(models map[string]config.ModelConfig, providers map[string]config.ProviderConfig, routingCfg config.RoutingConfig, latencyReader LatencyReader) *Router {
	providerRegions := make(map[string][]config.RegionConfig)
	regionByBaseURL := make(map[string]string)
	regionDataRes := make(map[string]string)

	// Collect all known combos for latency cache
	var combos []combo
	for providerName, pcfg := range providers {
		if len(pcfg.Regions) > 0 {
			providerRegions[providerName] = pcfg.Regions
			for _, r := range pcfg.Regions {
				normalized := normalizeURL(r.BaseURL)
				regionByBaseURL[normalized] = r.Name
				// Store data residency tag
				tag := r.DataResidency
				if tag == "" {
					tag = regionPrefix(r.Name)
				}
				regionDataRes[r.Name] = tag
			}
		}
	}

	// Build combos for all model routes + fallbacks with regions
	for _, mcfg := range models {
		allRoutes := append(mcfg.Routes, mcfg.Fallbacks...)
		for _, route := range allRoutes {
			if regions, ok := providerRegions[route.Provider]; ok {
				for _, r := range regions {
					combos = append(combos, combo{provider: route.Provider, model: route.Model, region: r.Name})
				}
			}
		}
	}

	strategy := strings.ToLower(routingCfg.RegionStrategy)
	if strategy == "" {
		strategy = "priority"
	}

	var cache *LatencyCache
	if strategy == "latency" && latencyReader != nil && len(combos) > 0 {
		cache = NewLatencyCache(latencyReader, combos, 10*time.Second)
	}

	return &Router{
		models:          models,
		providerRegions: providerRegions,
		regionByBaseURL: regionByBaseURL,
		regionDataRes:   regionDataRes,
		defaultRegion:   routingCfg.Region,
		strategy:        strategy,
		latencyCache:    cache,
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (r *Router) Plan(model string) (*Plan, error) {
	modelCfg, ok := r.models[model]
	if !ok {
		return nil, fmt.Errorf("model %q is not configured", model)
	}
	if len(modelCfg.Routes) == 0 {
		return nil, fmt.Errorf("model %q has no routes", model)
	}

	primary := chooseWeighted(r.rng, modelCfg.Routes)
	targets := r.resolveTargets(primary, modelCfg.Fallbacks)

	// Reorder targets: healthy first, unhealthy last (AC-03-02).
	targets = r.reorderByHealth(targets)

	return &Plan{Targets: targets}, nil
}

// SetHealthTracker sets the health checker used by Plan to deprioritize unhealthy
// targets. This is the injection point for health.Tracker (AUTH-05, AC-03-02).
// The Router's Plan() signature is unchanged (HB-05).
func (r *Router) SetHealthTracker(h HealthChecker) {
	r.health = h
}

// reorderByHealth partitions targets into healthy and unhealthy groups.
// Healthy targets come first; unhealthy targets come last.
// If ALL targets are unhealthy, the original order is preserved (best-effort).
func (r *Router) reorderByHealth(targets []providers.Target) []providers.Target {
	if r.health == nil || len(targets) <= 1 {
		return targets
	}

	var healthy, unhealthy []providers.Target
	for _, t := range targets {
		if r.health.IsHealthy(t.Provider, t.Model, t.Region) {
			healthy = append(healthy, t)
		} else {
			unhealthy = append(unhealthy, t)
		}
	}

	// Best-effort: if all unhealthy, keep original order.
	if len(healthy) == 0 {
		return targets
	}

	return append(healthy, unhealthy...)
}

// resolveTargets builds the full target list (primary + fallbacks) with region info.
func (r *Router) resolveTargets(primary config.ModelRoute, fallbacks []config.ModelRoute) []providers.Target {
	// Build primary targets — if the provider has regions, expand to regional targets
	primaryTargets := r.expandRoute(primary)
	if len(primaryTargets) == 0 {
		primaryTargets = []providers.Target{{Provider: primary.Provider, Model: primary.Model}}
	}

	// Pick the best primary based on strategy
	bestPrimary := r.selectBest(primaryTargets, primary.Model)

	// Build fallback targets
	var fallbackTargets []providers.Target
	for _, fb := range fallbacks {
		expanded := r.expandRoute(fb)
		if len(expanded) == 0 {
			fallbackTargets = append(fallbackTargets, providers.Target{Provider: fb.Provider, Model: fb.Model})
		} else {
			fallbackTargets = append(fallbackTargets, expanded...)
		}
	}

	targets := append([]providers.Target{bestPrimary}, fallbackTargets...)
	return targets
}

// expandRoute expands a single route into per-region targets if the provider has regions.
func (r *Router) expandRoute(route config.ModelRoute) []providers.Target {
	regions, ok := r.providerRegions[route.Provider]
	if !ok || len(regions) == 0 {
		return nil
	}

	targets := make([]providers.Target, 0, len(regions))
	for _, region := range regions {
		priority := region.Priority
		if priority <= 0 {
			priority = 1
		}
		targets = append(targets, providers.Target{
			Provider:      route.Provider,
			Model:         route.Model,
			Region:        region.Name,
			BaseURL:       region.BaseURL,
			DataResidency: r.regionDataRes[region.Name],
		})
	}
	return targets
}

// selectBest picks the best target from a set of regionalized targets.
func (r *Router) selectBest(targets []providers.Target, model string) providers.Target {
	if len(targets) == 1 {
		return targets[0]
	}

	switch r.strategy {
	case "latency":
		return r.selectByLatency(targets, model)
	default: // "priority"
		return r.selectByPriority(targets)
	}
}

// selectByPriority sorts targets by priority and prefers the local region as a tie-breaker.
func (r *Router) selectByPriority(targets []providers.Target) providers.Target {
	// Group by priority
	type prioritized struct {
		target   providers.Target
		priority int
		isLocal  bool
	}

	items := make([]prioritized, 0, len(targets))
	for _, t := range targets {
		p := r.regionPriority(t.Region)
		items = append(items, prioritized{
			target:   t,
			priority: p,
			isLocal:  r.defaultRegion != "" && t.Region == r.defaultRegion,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].priority != items[j].priority {
			return items[i].priority < items[j].priority
		}
		// Tie-break: prefer local region
		if items[i].isLocal != items[j].isLocal {
			return items[i].isLocal
		}
		return false
	})

	// Find all candidates with the best priority
	bestPriority := items[0].priority
	bestIsLocal := false
	for _, item := range items {
		if item.priority == bestPriority && item.isLocal {
			bestIsLocal = true
			break
		}
	}

	var candidates []providers.Target
	for _, item := range items {
		if item.priority != bestPriority {
			break
		}
		// If any local candidate exists at this priority, only keep local ones
		if bestIsLocal && !item.isLocal {
			continue
		}
		candidates = append(candidates, item.target)
	}

	if len(candidates) == 1 {
		return candidates[0]
	}
	// Random tie-break among equal candidates
	return candidates[r.rng.Intn(len(candidates))]
}

// selectByLatency picks the target with the lowest cached p50.
func (r *Router) selectByLatency(targets []providers.Target, model string) providers.Target {
	type scored struct {
		target  providers.Target
		latency time.Duration
		hasData bool
	}

	items := make([]scored, 0, len(targets))
	for _, t := range targets {
		if r.latencyCache != nil {
			if p50, ok := r.latencyCache.Get(t.Provider, t.Model, t.Region); ok {
				items = append(items, scored{target: t, latency: p50, hasData: true})
				continue
			}
		}
		// No data — assign worst priority (will be sorted last)
		items = append(items, scored{target: t, latency: time.Hour, hasData: false})
	}

	// If no targets have data, fall back to priority
	hasAnyData := false
	for _, item := range items {
		if item.hasData {
			hasAnyData = true
			break
		}
	}
	if !hasAnyData {
		return r.selectByPriority(targets)
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].latency < items[j].latency
	})

	return items[0].target
}

// regionPriority returns the priority for a region name from the provider config.
func (r *Router) regionPriority(regionName string) int {
	// Search all providers for this region
	for _, regions := range r.providerRegions {
		for _, region := range regions {
			if region.Name == regionName {
				p := region.Priority
				if p <= 0 {
					return 1
				}
				return p
			}
		}
	}
	return 999 // unknown region → lowest priority
}

func chooseWeighted(rng *rand.Rand, routes []config.ModelRoute) config.ModelRoute {
	if len(routes) == 1 {
		return routes[0]
	}

	total := 0
	for _, route := range routes {
		weight := route.Weight
		if weight <= 0 {
			weight = 1
		}
		total += weight
	}

	pick := rng.Intn(total)
	for _, route := range routes {
		weight := route.Weight
		if weight <= 0 {
			weight = 1
		}
		if pick < weight {
			return route
		}
		pick -= weight
	}

	return routes[0]
}

// normalizeURL trims trailing slashes and lowercases a URL for consistent matching.
func normalizeURL(u string) string {
	return strings.ToLower(strings.TrimRight(u, "/"))
}

// regionPrefix extracts the prefix before the first dash in a region name.
// e.g., "eu-west" → "eu", "us-east" → "us", "apac" → "apac"
func regionPrefix(name string) string {
	if idx := strings.Index(name, "-"); idx > 0 {
		return strings.ToLower(name[:idx])
	}
	return strings.ToLower(name)
}
