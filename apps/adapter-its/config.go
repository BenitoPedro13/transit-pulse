package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// version goes in the User-Agent.
const version = "0.1.0"

// maxRatePerSec is the politeness ceiling (invariant 4): 20% of the gateway's documented 5
// requests/s. Raising it needs the user's say-so, so LoadConfig refuses a higher value.
const maxRatePerSec = 1.0

// Config is read from the environment. The fields below the line are test knobs with fixed
// production defaults; they have no environment variable.
type Config struct {
	Brokers        []string
	RawDir         string
	BaseURL        string
	MinuteLag      time.Duration
	BackfillWindow time.Duration
	RatePerSec     float64
	Contact        string

	// ---
	RequestTimeout time.Duration // per HTTP attempt
	BackoffBase    time.Duration // first retry delay; doubles up to BackoffMax
	BackoffMax     time.Duration
	MaxAttempts    int           // per page
	FailedCooldown time.Duration // before a failed minute is tried again
	BadRequestWait time.Duration // before a minute that got a 4xx is tried again
	Now            func() time.Time
}

// defaultMinuteLag is measured, not guessed (docs/research/README.md §1.1): in 30 consecutive
// minutes the rows were identical at 30 s after the minute closed and at every later time
// tested. The smallest offset tested plus 30 s of margin is 60 s, which is also the documented
// "up to 60 s from reception to availability", so it cannot be shorter.
const defaultMinuteLag = time.Minute

func (c *Config) applyDefaults() {
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 45 * time.Second
	}
	if c.BackoffBase == 0 {
		c.BackoffBase = 2 * time.Second
	}
	if c.BackoffMax == 0 {
		c.BackoffMax = 60 * time.Second
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 5
	}
	if c.FailedCooldown == 0 {
		c.FailedCooldown = time.Minute
	}
	if c.BadRequestWait == 0 {
		c.BadRequestWait = 5 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// LoadConfig reads the configuration through getenv (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	dur := func(key string, def time.Duration) time.Duration {
		v := str(key, "")
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s=%q: want a positive Go duration such as 1m", key, v))
			return def
		}
		return d
	}

	c := Config{
		RawDir:         str("RAW_DIR", "./var/raw"),
		BaseURL:        strings.TrimRight(str("ITS_BASE_URL", "https://its.mobilidade.rio"), "/"),
		MinuteLag:      dur("ITS_MINUTE_LAG", defaultMinuteLag),
		BackfillWindow: dur("ITS_BACKFILL_WINDOW", 23*time.Hour),
		RatePerSec:     1,
		Contact:        str("ITS_CONTACT", ""),
	}
	for _, b := range strings.Split(str("KAFKA_BROKERS", "127.0.0.1:29092"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			c.Brokers = append(c.Brokers, b)
		}
	}
	if v := str("ITS_RATE_PER_SEC", ""); v != "" {
		r, err := strconv.ParseFloat(v, 64)
		switch {
		case err != nil || r <= 0:
			errs = append(errs, fmt.Errorf("ITS_RATE_PER_SEC=%q: want a positive number", v))
		case r > maxRatePerSec:
			errs = append(errs, fmt.Errorf("ITS_RATE_PER_SEC=%v is above the %v requests/s ceiling (invariant 4: be a polite client)", r, maxRatePerSec))
		default:
			c.RatePerSec = r
		}
	}
	if len(c.Brokers) == 0 {
		errs = append(errs, errors.New("KAFKA_BROKERS is empty"))
	}
	if c.BackfillWindow < time.Hour {
		errs = append(errs, fmt.Errorf("ITS_BACKFILL_WINDOW=%v is shorter than 1h", c.BackfillWindow))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	c.applyDefaults()
	return c, nil
}

// userAgent is "transit-pulse/<version> (+<contact>)"; with no contact there is no "(+…)" part.
func (c Config) userAgent() string {
	ua := "transit-pulse/" + version
	if c.Contact != "" {
		ua += " (+" + c.Contact + ")"
	}
	return ua
}
