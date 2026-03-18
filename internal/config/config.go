package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database DatabaseConfig `yaml:"database"`
	Redis   RedisConfig   `yaml:"redis"`
	App     AppConfig     `yaml:"app"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type AppConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Load 加载配置，优先级：.env > config.yaml > 默认值
func Load() (*Config, error) {
	cfg := defaultConfig()

	if err := loadYAML(cfg); err != nil {
		return nil, err
	}

	loadEnv(cfg)

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "root",
			Name:     "aegis_pay",
		},
		Redis: RedisConfig{
			Host: "localhost",
			Port: 6379,
			DB:   0,
		},
		App: AppConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
	}
}

func loadYAML(cfg *Config) error {
	yamlPaths := []string{
		"config.yaml",
		"./config.yaml",
		filepath.Join(getWorkDir(), "config.yaml"),
	}

	for _, path := range yamlPaths {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return fmt.Errorf("failed to parse yaml: %w", err)
			}
			return nil
		}
	}

	return nil
}

func loadEnv(cfg *Config) {
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("MYSQL_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Database.Port = port
		}
	}
	if v := os.Getenv("MYSQL_USER"); v != "" {
		cfg.Database.Username = v
	}
	if v := os.Getenv("MYSQL_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("MYSQL_DATABASE"); v != "" {
		cfg.Database.Name = v
	}

	if v := os.Getenv("REDIS_HOST"); v != "" {
		cfg.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Redis.Port = port
		}
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if db, err := strconv.Atoi(v); err == nil {
			cfg.Redis.DB = db
		}
	}

	if v := os.Getenv("APP_HOST"); v != "" {
		cfg.App.Host = v
	}
	if v := os.Getenv("APP_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.App.Port = port
		}
	}
}

func getWorkDir() string {
	dir, _ := os.Getwd()
	return dir
}

// GetDSN 获取 MySQL DSN 连接字符串
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.Username, c.Password, c.Host, c.Port, c.Name)
}

// GetAddr 获取 Redis 地址
func (c *RedisConfig) GetAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// GetAddr 获取应用地址
func (c *AppConfig) GetAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// LoadEnvFile 加载 .env 文件（如果存在）
func LoadEnvFile() {
	envPaths := []string{
		".env",
		"./.env",
		filepath.Join(getWorkDir(), ".env"),
	}

	for _, path := range envPaths {
		if data, err := os.ReadFile(path); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					if _, exists := os.LookupEnv(key); !exists {
						os.Setenv(key, value)
					}
				}
			}
			break
		}
	}
}
