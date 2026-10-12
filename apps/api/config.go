package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Config is read from the environment, like the adapter's and the consumer's.
type Config struct {
	DatabaseURL string
	Addr        string
	// HistoryHours is how many UTC hours of fixes the time machine keeps in memory.
	HistoryHours int
}

// LoadConfig reads the configuration through getenv (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		DatabaseURL: str("DATABASE_URL", ""),
		// Loopback only: Caddy (and `next dev`'s rewrite) is the way in, never the open network.
		Addr: str("API_ADDR", "127.0.0.1:8080"),
	}
	hours, err := strconv.Atoi(str("HISTORY_HOURS", strconv.Itoa(defaultHistoryHours)))
	if err != nil || hours < 1 || hours > maxHistoryHours {
		return Config{}, fmt.Errorf("HISTORY_HOURS must be a whole number from 1 to %d", maxHistoryHours)
	}
	c.HistoryHours = hours
	// No default: an API that silently read the wrong database is worse than one that refuses to
	// start.
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is empty (see .env.example)")
	}
	return c, nil
}
