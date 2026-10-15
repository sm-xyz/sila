package config

import (
	"os"
	"strconv"
)

type Config struct {
	Bind            string
	Port            string
	DBPath          string
	SecretKey       string
	PublicURL       string
	MetaAppID       string
	MetaAppSecret   string
	MetaVerifyToken string
	GoogleClientID  string
	GoogleSecret    string
	MaxConcurrency  int
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func LoadConfig() *Config {
	return &Config{
		Bind:            getEnv("SILA_BIND", "0.0.0.0"),
		Port:            getEnv("SILA_PORT", "8092"),
		DBPath:          getEnv("SILA_DB_PATH", "/opt/sila/data/sila.db"),
		SecretKey:       getEnv("SILA_SECRET_KEY", "sila_secret_internal_auth_key_64_chars_default_token"),
		PublicURL:       getEnv("SILA_PUBLIC_URL", "https://sila.domainanda.com"),
		MetaAppID:       getEnv("META_APP_ID", ""),
		MetaAppSecret:   getEnv("META_APP_SECRET", ""),
		MetaVerifyToken: getEnv("META_VERIFY_TOKEN", "sila_meta_webhook_verify_token_default"),
		GoogleClientID:  getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleSecret:    getEnv("GOOGLE_CLIENT_SECRET", ""),
		MaxConcurrency:  getEnvInt("SILA_MAX_CONCURRENCY", 2),
	}
}
