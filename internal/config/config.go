package config

import (
	"fmt"
	"os"
)

type Config struct {
	RabbitMQURL        string
	RabbitMQQueue      string
	APIHTTPPort        string
	APIGRPCPort        string
	APIGRPCAddr        string
	WorkerDispatchPort string
}

// Load reads config from environment variables with sensible defaults.
// In development, values come from a .env file loaded by the shell or Docker.
func Load() *Config {
	return &Config{
		RabbitMQURL:        getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RabbitMQQueue:      getEnv("RABBITMQ_QUEUE", "jobs"),
		APIHTTPPort:        getEnv("API_HTTP_PORT", "8081"),
		APIGRPCPort:        getEnv("API_GRPC_PORT", "50051"),
		APIGRPCAddr:        getEnv("API_GRPC_ADDR", "localhost:50051"),
		WorkerDispatchPort: getEnv("WORKER_DISPATCH_PORT", "9001"),
	}
}

func (c *Config) HTTPAddr() string {
	return fmt.Sprintf(":%s", c.APIHTTPPort)
}

func (c *Config) GRPCAddr() string {
	return fmt.Sprintf(":%s", c.APIGRPCPort)
}

func (c *Config) DispatchAddr() string {
	return fmt.Sprintf(":%s", c.WorkerDispatchPort)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
