package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 历史默认值，首次启动检测到这个值必须替换为随机串
const legacyDefaultJWTSecret = "dnsplane-secret-key-change-me"

type Config struct {
	Server     ServerConfig     `json:"server"`
	Database   DatabaseConfig   `json:"database"`
	JWT        JWTConfig        `json:"jwt"`
	Proxy      ProxyConfig      `json:"proxy"`
	LogCleanup LogCleanupConfig `json:"log_cleanup"`
	Redis      RedisConfig      `json:"redis"`
	Security   SecurityConfig   `json:"security"`
}

// SecurityConfig 存储字段级加密主密钥；允许从环境变量 DNSPLANE_MASTER_KEY 覆盖
type SecurityConfig struct {
	MasterKey string `json:"master_key"`
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
					// 配置文件不存在，生成随机 JWT 密钥与字段加密主密钥，创建默认配置
					cfg.JWT.Secret = generateRandomSecret(32)
					cfg.Security.MasterKey = generateRandomSecret(32)
					err = saveConfig(path, cfg)
					return
				}
				return
			}
			err = json.Unmarshal(data, cfg)
			if err != nil {
				return
			}
		}

		// 环境变量优先：DNSPLANE_JWT_SECRET / DNSPLANE_MASTER_KEY 用于容器化部署
		if v := strings.TrimSpace(os.Getenv("DNSPLANE_JWT_SECRET")); v != "" {
			cfg.JWT.Secret = v
		}
		if v := strings.TrimSpace(os.Getenv("DNSPLANE_MASTER_KEY")); v != "" {
			cfg.Security.MasterKey = v
		}

		// 历史默认 secret 或空值自动替换并回写，防止生产环境使用公开默认值
		dirty := false
		if s := strings.TrimSpace(cfg.JWT.Secret); s == "" || s == legacyDefaultJWTSecret {
			cfg.JWT.Secret = generateRandomSecret(32)
			dirty = true
		}
		if strings.TrimSpace(cfg.Security.MasterKey) == "" {
			cfg.Security.MasterKey = generateRandomSecret(32)
			dirty = true
		}
		if dirty && path != "" {
			// 回写失败不阻断启动，只影响下次重启再次生成
			_ = saveConfig(path, cfg)
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
		// 0700：仅 owner 可访问配置目录（含密钥）
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600：仅 owner 可读写，防止同机其他用户读取 JWT secret / master_key / DB 密码（安全审计 M-5）
	return os.WriteFile(path, data, 0600)
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
	// 0600：仅 owner 可读写，防止同机其他用户读取 JWT secret / master_key / DB 密码（安全审计 M-5）
	return os.WriteFile(path, data, 0600)
}
