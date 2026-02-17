package session

// QuotaConfig holds the configured quota limits for the session service.
// These can be loaded from the provider config file or set programmatically.
type QuotaConfig struct {
	// DefaultMaxConcurrentSessions is the default maximum number of concurrent
	// sessions allowed per payer when no per-payer override exists.
	DefaultMaxConcurrentSessions int

	// DefaultMaxWorkersPerSession is the default maximum number of concurrent
	// workers (streaming connections) allowed per session.
	DefaultMaxWorkersPerSession int

	// PerPayerOverrides maps a payer address (lowercase hex 0x...) to its
	// specific quota limits. Overrides take precedence over the defaults.
	PerPayerOverrides map[string]*PayerQuota
}

// PayerQuota holds per-payer quota overrides.
type PayerQuota struct {
	MaxConcurrentSessions int
	MaxWorkersPerSession  int
}

// DefaultQuotaConfig returns a sensible default QuotaConfig.
func DefaultQuotaConfig() *QuotaConfig {
	return &QuotaConfig{
		DefaultMaxConcurrentSessions: 10,
		DefaultMaxWorkersPerSession:  5,
		PerPayerOverrides:            make(map[string]*PayerQuota),
	}
}

// MaxConcurrentSessions returns the effective maximum number of concurrent
// sessions for the given payer address.
func (c *QuotaConfig) MaxConcurrentSessions(payer string) int {
	if override, ok := c.PerPayerOverrides[payer]; ok {
		return override.MaxConcurrentSessions
	}
	return c.DefaultMaxConcurrentSessions
}

// MaxWorkersPerSession returns the effective maximum number of workers
// per session for the given payer address.
func (c *QuotaConfig) MaxWorkersPerSession(payer string) int {
	if override, ok := c.PerPayerOverrides[payer]; ok {
		return override.MaxWorkersPerSession
	}
	return c.DefaultMaxWorkersPerSession
}
