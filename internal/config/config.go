package config

import (
	"fmt"
	"nemi/internal/connectors"
	"os"
	"strconv"
)

type Config struct {
	DB, Temporal, Listen, Origin, Invite, Provider, Model, Key                             string
	InputPrice, OutputPrice                                                                int64
	Bots                                                                                   map[string]connectors.Bot
	RunQueue, ReminderQueue                                                                string
	VaultKey, VaultPath                                                                    string
	BootstrapID, OutboxWorkspace                                                           string
	FilesRoot, FilesS3Endpoint, FilesS3Bucket, FilesS3Access, FilesS3Secret, FilesS3Region string
	FilesS3Secure                                                                          bool
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func Load() (Config, error) {
	c := Config{DB: os.Getenv("DATABASE_URL"), Temporal: env("TEMPORAL_ADDRESS", "localhost:7233"), Listen: env("APP_LISTEN", "127.0.0.1:8080"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Invite: os.Getenv("APP_INVITE_CODE"), Provider: os.Getenv("MODEL_PROVIDER"), Model: os.Getenv("MODEL_NAME"), Key: os.Getenv("MODEL_API_KEY")}
	if env("APP_ENV", "development") != "development" {
		return c, fmt.Errorf("this first slice supports private development only; production identity is not implemented")
	}
	c.Bots = connectors.LoadEnv()
	c.BootstrapID = env("APP_BOOTSTRAP_ID", "local-owner")
	c.OutboxWorkspace = os.Getenv("APP_OUTBOX_WORKSPACE")
	c.VaultKey = os.Getenv("APP_CREDENTIAL_KEY")
	c.VaultPath = env("APP_CREDENTIAL_KEY_FILE", "data/credentials.key")
	c.FilesRoot = env("APP_FILES_ROOT", "data/files")
	c.FilesS3Endpoint, c.FilesS3Bucket = os.Getenv("FILES_S3_ENDPOINT"), os.Getenv("FILES_S3_BUCKET")
	c.FilesS3Access, c.FilesS3Secret = os.Getenv("FILES_S3_ACCESS_KEY"), os.Getenv("FILES_S3_SECRET_KEY")
	c.FilesS3Region = env("FILES_S3_REGION", "us-east-1")
	c.FilesS3Secure = env("FILES_S3_SECURE", "true") != "false"
	if c.FilesS3Endpoint != "" && (c.FilesS3Bucket == "" || c.FilesS3Access == "" || c.FilesS3Secret == "") {
		return c, fmt.Errorf("complete S3 file storage configuration required")
	}
	c.RunQueue = env("TEMPORAL_RUN_QUEUE", "nemi-runtime-v1")
	c.ReminderQueue = env("TEMPORAL_REMINDER_QUEUE", "nemi-notification-v1")
	if c.DB == "" || len(c.Invite) < 16 {
		return c, fmt.Errorf("DATABASE_URL and an APP_INVITE_CODE of at least 16 characters are required")
	}
	// Without a server credential, users configure their own model in the UI.
	if c.Key == "" {
		c.Provider = ""
		c.Model = ""
	}
	if c.Provider != "" && c.Provider != "qwen" && c.Provider != "deepseek" && c.Provider != "kimi" && c.Provider != "doubao" && c.Provider != "glm" {
		return c, fmt.Errorf("unsupported model provider")
	}
	if c.Provider != "" {
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
