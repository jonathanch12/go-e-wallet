package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server      ServerConfig
	Database    DatabaseConfig
	JWTSecret   string
	CoreService CoreServiceConfig
	RabbitMQ    RabbitMQConfig
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type CoreServiceConfig struct {
	BaseURL     string
	InternalKey string
	Timeout     time.Duration
}

type RabbitMQConfig struct {
	URL                string
	Exchange           string
	Queue              string
	RoutingKey         string
	DeadLetterExchange string
	DeadLetterQueue    string
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8081"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", ""),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", ""),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		JWTSecret: getEnv("JWT_SECRET", ""),
		CoreService: CoreServiceConfig{
			BaseURL:     getEnv("CORE_SERVICE_URL", "http://localhost:8080"),
			InternalKey: getEnv("INTERNAL_API_KEY", ""),
		},
		RabbitMQ: RabbitMQConfig{
			URL:                getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
			Exchange:           getEnv("RABBITMQ_EXCHANGE", "payment.exchange"),
			Queue:              getEnv("RABBITMQ_QUEUE", "payment.queue"),
			RoutingKey:         getEnv("RABBITMQ_ROUTING_KEY", "payment.process"),
			DeadLetterExchange: getEnv("RABBITMQ_DLX", "payment.dlx"),
			DeadLetterQueue:    getEnv("RABBITMQ_DLQ", "payment.queue.dlq"),
		},
	}

	timeout, err := time.ParseDuration(getEnv("CORE_SERVICE_TIMEOUT", "5s"))
	if err != nil {
		return nil, fmt.Errorf("invalid CORE_SERVICE_TIMEOUT: %w", err)
	}
	cfg.CoreService.Timeout = timeout

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	missing := make([]string, 0)
	if c.Database.User == "" {
		missing = append(missing, "DB_USER")
	}
	if c.Database.Name == "" {
		missing = append(missing, "DB_NAME")
	}
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.CoreService.InternalKey == "" {
		missing = append(missing, "INTERNAL_API_KEY")
	}
	if c.RabbitMQ.URL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %v", missing)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
