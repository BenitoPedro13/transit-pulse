package main

import (
	"errors"
	"strings"
)

// Config is read from the environment, like the adapter's.
type Config struct {
	Brokers     []string
	DatabaseURL string
}

// LoadConfig reads the configuration through getenv (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	var c Config
	for _, b := range strings.Split(str("KAFKA_BROKERS", "127.0.0.1:29092"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			c.Brokers = append(c.Brokers, b)
		}
	}
	c.DatabaseURL = str("DATABASE_URL", "")
	var errs []error
	if len(c.Brokers) == 0 {
		errs = append(errs, errors.New("KAFKA_BROKERS is empty"))
	}
	// No default: a consumer that silently wrote to the wrong database is worse than one that
	// refuses to start.
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is empty (see .env.example)"))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return c, nil
}
