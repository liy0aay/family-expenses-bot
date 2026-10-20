package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr        string
	Postgres          Postgres
	LLM               LLM
	TelegramBotToken  string
	ExpensesListLimit int
	DialogWindow      int
	DialogTTL         time.Duration
}

type Postgres struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

// DSN builds a pgx keyword-value string; values with spaces are not supported.
func (p Postgres) DSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		p.Host, p.Port, p.User, p.Password, p.Name, p.SSLMode)
}

type LLM struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
}

func Load() (Config, error) {
	var cfg Config

	cfg.ListenAddr = envString("LISTEN_ADDR", ":8080")

	cfg.Postgres = Postgres{
		Host:     envString("POSTGRES_HOST", "localhost"),
		Port:     envString("POSTGRES_PORT", "5432"),
		User:     envString("POSTGRES_USER", "postgres"),
		Password: envString("POSTGRES_PASSWORD", "postgres"),
		Name:     envString("POSTGRES_DB", "expenses"),
		SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
	}

	cfg.LLM = LLM{
		BaseURL: envString("LLM_BASE_URL", ""),
		APIKey:  envString("LLM_API_KEY", ""),
		Model:   envString("LLM_MODEL", ""),
	}

	temperature, err := envFloat("LLM_TEMPERATURE", 0.2)
	if err != nil {
		return Config{}, err
	}
	cfg.LLM.Temperature = temperature

	// default is aligned with the maximum of the limit query parameter in openapi
	limit, err := envInt("EXPENSES_LIST_LIMIT", 500)
	if err != nil {
		return Config{}, err
	}
	if limit <= 0 {
		return Config{}, fmt.Errorf("EXPENSES_LIST_LIMIT: must be positive, got %d", limit)
	}
	cfg.ExpensesListLimit = limit

	window, err := envInt("DIALOG_WINDOW", 16)
	if err != nil {
		return Config{}, err
	}
	if window <= 0 {
		return Config{}, fmt.Errorf("DIALOG_WINDOW: must be positive, got %d", window)
	}
	cfg.DialogWindow = window

	ttl := envString("DIALOG_TTL", "30m")
	duration, err := time.ParseDuration(ttl)
	if err != nil {
		return Config{}, fmt.Errorf("DIALOG_TTL: Go duration expected (e.g. 30m), got %q", ttl)
	}
	if duration <= 0 {
		return Config{}, fmt.Errorf("DIALOG_TTL: must be positive, got %s", ttl)
	}
	cfg.DialogTTL = duration

	cfg.TelegramBotToken = envString("TELEGRAM_BOT_TOKEN", "")

	return cfg, nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: integer expected, got %q", key, v)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: number expected, got %q", key, v)
	}
	return f, nil
}
