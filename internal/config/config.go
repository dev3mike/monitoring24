package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Config holds all runtime configuration.
type Config struct {
	Host          string
	Port          int
	DataDir       string
	BasicAuthUser string
	BasicAuthHash []byte   // bcrypt hash — nil means auth disabled
	Services      []string // systemd services to monitor
	LogLevel      string
}

func (c *Config) AuthEnabled() bool {
	return c.BasicAuthHash != nil
}

// Addr returns the listen address string.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// ParseFlags parses CLI flags and returns a Config.
func ParseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.Host, "host", "0.0.0.0", "bind address")
	flag.IntVar(&cfg.Port, "port", 47291, "HTTP port")
	flag.StringVar(&cfg.DataDir, "data-dir", "./data", "directory for database and data files")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "log level: debug, info, warn")

	var basicAuth string
	flag.StringVar(&basicAuth, "basic-auth", "", "optional HTTP basic auth in user:password format")

	var services string
	flag.StringVar(&services, "services", "", "comma-separated systemd service names to monitor (default: nginx,postgresql,...)")

	flag.Parse()

	// Hash basic auth password immediately — never store plaintext
	if basicAuth != "" {
		user, pass, ok := strings.Cut(basicAuth, ":")
		if !ok || user == "" || pass == "" {
			log.Fatal("--basic-auth must be in user:password format")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("bcrypt error: %v", err)
		}
		cfg.BasicAuthUser = user
		cfg.BasicAuthHash = hash
	}

	if services != "" {
		for _, s := range strings.Split(services, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.Services = append(cfg.Services, s)
			}
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	return cfg
}
