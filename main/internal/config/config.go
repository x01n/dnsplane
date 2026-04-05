package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Server     ServerConfig     `json:"server"`
	Database   DatabaseConfig   `json:"database"`
	JWT        JWTConfig        `json:"jwt"`
	Proxy      ProxyConfig      `json:"proxy"`
	LogCleanup LogCleanupConfig `json:"log_cleanup"`
	Redis      RedisConfig      `json:"redis"`
}

type RedisConfig struct {
	Enable       bool   `json:"enable"`
	Addr         string `json:"addr"` // host:port, e.g. "127.0.0.1:6379"
	Password     string `json:"password"`
	DB           int    `json:"db"`
	PoolSize     int    `json:"pool_size"`      // 连接池大小，默认 10
	MinIdleConns int    `json:"min_idle_conns"` // 最小空闲连接
	KeyPrefix    string `json:"key_prefix"`     // 逻辑 key 前缀，建议如 "dnsplane:" 避免多环境冲突
}

type LogCleanupConfig struct {
	Enable           bool `json:"enable"`             // 是否启用自动清理
	SuccessKeepCount int  `json:"success_keep_count"` // 保留成功日志条数
	ErrorKeepCount   int  `json:"error_keep_count"`   // 保留错误日志条数
	CleanupInterval  int  `json:"cleanup_interval"`   // 清理间隔(小时)
}

type ServerConfig struct {
	Port    int    `json:"port"`
	Host    string `json:"host"`
	Mode    string `json:"mode"` // debug, release
	BaseURL string `json:"base_url"`
}

type DatabaseConfig struct {
	Driver   string `json:"driver"` // sqlite, mysql
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	FilePath string `json:"file_path"` // for sqlite
}

// LogDBPath 从主数据库路径推导日志数据库路径
func (c *DatabaseConfig) LogDBPath() string {
	dir := filepath.Dir(c.FilePath)
	return filepath.Join(dir, "logs.db")
}

// RequestDBPath 从主数据库路径推导请求日志数据库路径
func (c *DatabaseConfig) RequestDBPath() string {
	dir := filepath.Dir(c.FilePath)
	return filepath.Join(dir, "request_logs.db")
}

type JWTConfig struct {
	Secret     string `json:"secret"`
	ExpireHour int    `json:"expire_hour"`
}

type ProxyConfig struct {
	Enable bool   `json:"enable"`
	URL    string `json:"url"`
}

var (
	cfg  *Config
	once sync.Once
)

func Load(path string) (*Config, error) {
	var err error
	once.Do(func() {
		cfg = &Config{
			Server: ServerConfig{
				Port: 8080,
				Host: "0.0.0.0",
				Mode: "release",
			},
			Database: DatabaseConfig{
				Driver:   "sqlite",
				FilePath: "data/dnsplane.db",
			},
			JWT: JWTConfig{
				Secret:     "dnsplane-secret-key-change-me",
				ExpireHour: 24,
			},
			LogCleanup: LogCleanupConfig{
				Enable:           true,
				SuccessKeepCount: 1000,
				ErrorKeepCount:   500,
				CleanupInterval:  6,
			},
		}

		if path != "" {
			var data []byte
			data, err = os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					// 配置文件不存在，生成随机JWT密钥并创建默认配置
					cfg.JWT.Secret = generateRandomSecret(32)
					err = saveConfig(path, cfg)
					return
				}
				return
			}
			err = json.Unmarshal(data, cfg)
		}
	})
	return cfg, err
}

func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "dnsplane-fallback-secret-key"
	}
	return hex.EncodeToString(bytes)
}

func saveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func Get() *Config {
	if cfg == nil {
		cfg, _ = Load("")
	}
	return cfg
}

func Save(path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
