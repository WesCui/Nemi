package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DB, Temporal, Listen, Origin, Invite, Provider, Model, Key string
	InputPrice, OutputPrice                                    int64
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func Load() (Config, error) {
	c := Config{DB: os.Getenv("DATABASE_URL"), Temporal: env("TEMPORAL_ADDRESS", "localhost:7233"), Listen: env("APP_LISTEN", "127.0.0.1:8080"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Invite: os.Getenv("APP_INVITE_CODE"), Provider: env("MODEL_PROVIDER", "demo"), Model: os.Getenv("MODEL_NAME"), Key: os.Getenv("MODEL_API_KEY")}
	if env("APP_ENV", "development") != "development" {
		return c, fmt.Errorf("this first slice supports private development only; production identity is not implemented")
	}
	if c.DB == "" || len(c.Invite) < 16 {
		return c, fmt.Errorf("DATABASE_URL and an APP_INVITE_CODE of at least 16 characters are required")
	}
	if c.Provider != "demo" && c.Provider != "qwen" && c.Provider != "deepseek" {
		return c, fmt.Errorf("unsupported model provider")
	}
	if c.Provider != "demo" {
		var e error
		c.InputPrice, e = strconv.ParseInt(os.Getenv("MODEL_INPUT_PRICE_MICRO_CNY"), 10, 64)
		if e != nil || c.InputPrice <= 0 || c.InputPrice > 1000000000 {
			return c, fmt.Errorf("valid input token price required")
		}
		c.OutputPrice, e = strconv.ParseInt(os.Getenv("MODEL_OUTPUT_PRICE_MICRO_CNY"), 10, 64)
		if e != nil || c.OutputPrice <= 0 || c.OutputPrice > 1000000000 {
			return c, fmt.Errorf("valid output token price required")
		}
		if c.Model == "" || c.Key == "" {
			return c, fmt.Errorf("MODEL_NAME and MODEL_API_KEY required")
		}
	}
	return c, nil
}
