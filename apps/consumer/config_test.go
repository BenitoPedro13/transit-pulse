package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	c, err := LoadConfig(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil || len(c.Brokers) != 1 || c.Brokers[0] != "127.0.0.1:29092" || c.DatabaseURL != "postgres://x" {
		t.Errorf("defaults = %+v, %v", c, err)
	}
	c, err = LoadConfig(env(map[string]string{"DATABASE_URL": "postgres://x", "KAFKA_BROKERS": " a:1, b:2 ,"}))
	if err != nil || len(c.Brokers) != 2 || c.Brokers[1] != "b:2" {
		t.Errorf("brokers = %+v, %v", c, err)
	}
	// No default database: refusing to start beats writing to the wrong one.
	if _, err := LoadConfig(env(nil)); err == nil {
		t.Error("an empty DATABASE_URL was accepted")
	}
}

func TestEnvExampleListsEveryVariable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"KAFKA_BROKERS", "DATABASE_URL"} {
		if !strings.Contains(string(b), k+"=") {
			t.Errorf(".env.example does not list %s", k)
		}
	}
}
