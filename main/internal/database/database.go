package database

import (
	"context"
	"fmt"
	"main/internal/config"
	"main/internal/models"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB        *gorm.DB // 主数据库
	LogDB     *gorm.DB // 日志数据库 (Log, CertLog, DMCheckLog)
	RequestDB *gorm.DB // 请求日志数据库 (RequestLog)
)

// IsSQLite 主库是否为 SQLite（用于拼接/函数方言差异）
func IsSQLite() bool {
	if DB == nil {
		return false
	}
	return DB.Dialector.Name() == "sqlite"
}

// optimizeSQLite 对SQLite数据库进行性能优化
func optimizeSQLite(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	sqlDB.Exec("PRAGMA journal_mode=WAL")
	sqlDB.Exec("PRAGMA synchronous=NORMAL")
	sqlDB.Exec("PRAGMA cache_size=-64000") // 64MB cache
	sqlDB.Exec("PRAGMA busy_timeout=5000")
	sqlDB.Exec("PRAGMA temp_store=MEMORY")
	// 只读路径加速（Windows/Linux 均支持；若驱动报错可忽略）
	_, _ = sqlDB.Exec("PRAGMA mmap_size=67108864") // 64MiB
}

// applySQLiteConnPool WAL 下允许多连接并发读，避免默认池过小导致请求在 Go 侧排队
func applySQLiteConnPool(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	sqlDB.SetMaxOpenConns(64)
	sqlDB.SetMaxIdleConns(32)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(0)
}

// applyMySQLConnPool 提高默认并发下的连接复用
func applyMySQLConnPool(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(time.Hour)
}

func Init(cfg *config.DatabaseConfig) error {
	var err error

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	switch cfg.Driver {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
		DB, err = gorm.Open(mysql.Open(dsn), gormConfig)
		if err != nil {
			return fmt.Errorf("连接MySQL数据库失败: %w", err)
		}
		applyMySQLConnPool(DB)
	case "sqlite":
		fallthrough
	default:
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建数据目录失败: %w", err)
		}
		dsn := cfg.FilePath + "?_busy_timeout=5000&_journal_mode=WAL"
		DB, err = gorm.Open(sqlite.Open(dsn), gormConfig)
		if err != nil {
			return fmt.Errorf("连接SQLite数据库失败: %w", err)
		}
		optimizeSQLite(DB)
		applySQLiteConnPool(DB)
	}

	// 初始化日志数据库 (LogDB)
	logDBPath := cfg.LogDBPath()
	logDSN := logDBPath + "?_busy_timeout=5000&_journal_mode=WAL"
	LogDB, err = gorm.Open(sqlite.Open(logDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("连接日志数据库失败: %w", err)
	}
	optimizeSQLite(LogDB)
	applySQLiteConnPool(LogDB)

	// 初始化请求日志数据库 (RequestDB)
	requestDBPath := cfg.RequestDBPath()
	requestDSN := requestDBPath + "?_busy_timeout=5000&_journal_mode=WAL"
	RequestDB, err = gorm.Open(sqlite.Open(requestDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("连接请求日志数据库失败: %w", err)
	}
	optimizeSQLite(RequestDB)
	applySQLiteConnPool(RequestDB)

	if err := migrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	if err := migrateLogDB(); err != nil {
		return fmt.Errorf("日志数据库迁移失败: %w", err)
	}

	if err := migrateRequestDB(); err != nil {
		return fmt.Errorf("请求日志数据库迁移失败: %w", err)
	}

	if err := initAdmin(); err != nil {
		return fmt.Errorf("初始化管理员失败: %w", err)
	}

	// 迁移旧数据：将主库中的日志数据迁移到独立数据库
	migrateOldData()

	return nil
}

// migrateOldData 将旧主库中的日志数据迁移到新的独立数据库并清理旧表
func migrateOldData() {
	// 检查主库中是否存在 logs 表（旧数据）
	if DB.Migrator().HasTable("logs") {
		var count int64
		DB.Raw("SELECT COUNT(*) FROM logs").Scan(&count)
		if count > 0 {
			fmt.Printf("正在迁移 %d 条操作日志到独立数据库...\n", count)
			// 批量迁移
			var logs []models.Log
			DB.Raw("SELECT * FROM logs").Scan(&logs)
			if len(logs) > 0 {
				// 分批写入（每批500条）
				batchSize := 500
				for i := 0; i < len(logs); i += batchSize {
					end := i + batchSize
					if end > len(logs) {
						end = len(logs)
					}
					LogDB.Create(logs[i:end])
				}
				fmt.Printf("操作日志迁移完成: %d 条\n", len(logs))
			}
			// 清理旧表数据
			DB.Exec("DELETE FROM logs")
			DB.Exec("VACUUM")
			fmt.Println("旧操作日志表已清理")
		}
		// 删除旧表
		DB.Exec("DROP TABLE IF EXISTS logs")
	}

	// 检查主库中是否存在 cert_logs 表（旧数据）
	if DB.Migrator().HasTable("cert_logs") {
		var count int64
		DB.Raw("SELECT COUNT(*) FROM cert_logs").Scan(&count)
		if count > 0 {
			fmt.Printf("正在迁移 %d 条证书日志到独立数据库...\n", count)
			var certLogs []models.CertLog
			DB.Raw("SELECT * FROM cert_logs").Scan(&certLogs)
			if len(certLogs) > 0 {
				batchSize := 500
				for i := 0; i < len(certLogs); i += batchSize {
					end := i + batchSize
					if end > len(certLogs) {
						end = len(certLogs)
					}
					LogDB.Create(certLogs[i:end])
				}
				fmt.Printf("证书日志迁移完成: %d 条\n", len(certLogs))
			}
			DB.Exec("DELETE FROM cert_logs")
			DB.Exec("DROP TABLE IF EXISTS cert_logs")
		}
	}

	// 检查主库中是否存在 request_logs 表（旧数据）
	if DB.Migrator().HasTable("request_logs") {
		var count int64
		DB.Raw("SELECT COUNT(*) FROM request_logs").Scan(&count)
		if count > 0 {
			fmt.Printf("正在迁移 %d 条请求日志到独立数据库...\n", count)
			var requestLogs []models.RequestLog
			DB.Raw("SELECT * FROM request_logs").Scan(&requestLogs)
			if len(requestLogs) > 0 {
				batchSize := 500
				for i := 0; i < len(requestLogs); i += batchSize {
					end := i + batchSize
					if end > len(requestLogs) {
						end = len(requestLogs)
					}
					RequestDB.Create(requestLogs[i:end])
				}
				fmt.Printf("请求日志迁移完成: %d 条\n", len(requestLogs))
			}
			DB.Exec("DELETE FROM request_logs")
			DB.Exec("DROP TABLE IF EXISTS request_logs")
			DB.Exec("VACUUM")
		}
	}
}

func migrate() error {
	if err := DB.AutoMigrate(
		&models.User{},
		&models.UserOAuth{},
		&models.Account{},
		&models.Domain{},
		&models.DomainNote{},
		&models.Permission{},
		&models.DMTask{},
		&models.DMLog{},
		&models.CertAccount{},
		&models.CertOrder{},
		&models.CertDomain{},
		&models.CertDeploy{},
		&models.CertCNAME{},
		&models.ScheduleTask{},
		&models.SysConfig{},
		&models.OptimizeIP{},
	); err != nil {
		return err
	}

	// 迁移旧 github_id 数据到 user_oauth 表
	migrateGitHubIDToOAuth()
	return nil
}

// migrateGitHubIDToOAuth 将 User.GitHubID 迁移到 UserOAuth 表
func migrateGitHubIDToOAuth() {
	var users []models.User
	DB.Where("github_id > 0").Find(&users)
	for _, u := range users {
		var existing models.UserOAuth
		if DB.Where("provider = ? AND provider_user_id = ?", "github", fmt.Sprintf("%d", u.GitHubID)).First(&existing).Error == nil {
			continue // 已迁移
		}
		DB.Create(&models.UserOAuth{
			UserID:         u.ID,
			Provider:       "github",
			ProviderUserID: fmt.Sprintf("%d", u.GitHubID),
			ProviderName:   u.Username,
			ProviderEmail:  u.Email,
			CreatedAt:      u.CreatedAt,
		})
	}
}

func migrateLogDB() error {
	return LogDB.AutoMigrate(
		&models.Log{},
		&models.CertLog{},
		&models.DMCheckLog{},
	)
}

func migrateRequestDB() error {
	return RequestDB.AutoMigrate(
		&models.RequestLog{},
	)
}

func initAdmin() error {
	var count int64
	DB.Model(&models.User{}).Count(&count)
	if count > 0 {
		DB.Model(&models.User{}).Where("id = 1 AND level < 2").Update("level", 2)
		return nil
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	admin := models.User{
		Username: "admin",
		Password: string(hashedPassword),
		Level:    2, // 2=管理员
		Status:   1,
		RegTime:  time.Now(),
	}

	if err := DB.Create(&admin).Error; err != nil {
		return err
	}

	fmt.Println("已创建默认管理员账户: admin / admin123")
	return nil
}

func Close() error {
	if DB != nil {
		if sqlDB, err := DB.DB(); err == nil {
			sqlDB.Close()
		}
	}
	if LogDB != nil {
		if sqlDB, err := LogDB.DB(); err == nil {
			sqlDB.Close()
		}
	}
	if RequestDB != nil {
		if sqlDB, err := RequestDB.DB(); err == nil {
			sqlDB.Close()
		}
	}
	return nil
}

// DBQuery 数据库查询记录
type DBQuery struct {
	SQL      string `json:"sql"`
	Duration int64  `json:"duration_ms"`
	Rows     int64  `json:"rows"`
	Error    string `json:"error,omitempty"`
}

const dbQueriesKey = "db_queries"
const dbStartTimeKey = "db_start_time"

// WithContext 为DB添加请求上下文
func WithContext(c *gin.Context) *gorm.DB {
	return DB.WithContext(context.WithValue(c.Request.Context(), "gin_context", c))
}

// WithLogContext 返回日志数据库连接（Log queries不需要请求追踪）
func WithLogContext(c *gin.Context) *gorm.DB {
	return LogDB
}

// WithRequestContext 返回请求日志数据库连接
func WithRequestContext(c *gin.Context) *gorm.DB {
	return RequestDB
}

// RegisterDBCallbacks 注册GORM查询回调
func RegisterDBCallbacks() {
	// Query回调 (SELECT)
	DB.Callback().Query().Before("gorm:query").Register("record_start_time", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Query().After("gorm:query").Register("record_query", recordQueryCallback)

	// Create回调 (INSERT)
	DB.Callback().Create().Before("gorm:create").Register("record_start_time_create", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Create().After("gorm:create").Register("record_create", recordQueryCallback)

	// Update回调 (UPDATE)
	DB.Callback().Update().Before("gorm:update").Register("record_start_time_update", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Update().After("gorm:update").Register("record_update", recordQueryCallback)

	// Delete回调 (DELETE)
	DB.Callback().Delete().Before("gorm:delete").Register("record_start_time_delete", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Delete().After("gorm:delete").Register("record_delete", recordQueryCallback)

	// Row回调 (用于Scan等操作)
	DB.Callback().Row().Before("gorm:row").Register("record_start_time_row", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Row().After("gorm:row").Register("record_row", recordQueryCallback)

	// Raw回调 (用于Exec等原生SQL)
	DB.Callback().Raw().Before("gorm:raw").Register("record_start_time_raw", func(db *gorm.DB) {
		db.Set(dbStartTimeKey, time.Now())
	})
	DB.Callback().Raw().After("gorm:raw").Register("record_raw", recordQueryCallback)
}

func recordQueryCallback(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Context == nil {
		return
	}

	// 获取gin上下文
	ginCtx, ok := db.Statement.Context.Value("gin_context").(*gin.Context)
	if !ok || ginCtx == nil {
		return
	}

	// 获取开始时间
	startTime, ok := db.Get(dbStartTimeKey)
	if !ok {
		return
	}

	duration := time.Since(startTime.(time.Time)).Milliseconds()

	// 获取SQL
	var sql string
	if db.Statement.SQL.String() != "" {
		sql = db.Dialector.Explain(db.Statement.SQL.String(), db.Statement.Vars...)
	}

	// 如果SQL为空，尝试构建基本信息
	if sql == "" {
		table := db.Statement.Table
		if table == "" && db.Statement.Schema != nil {
			table = db.Statement.Schema.Table
		}
		if table != "" {
			sql = fmt.Sprintf("[%s] table: %s, rows: %d", db.Statement.ReflectValue.Kind().String(), table, db.RowsAffected)
		}
	}

	// 跳过空SQL
	if sql == "" {
		return
	}

	// 截断过长的SQL
	if len(sql) > 2000 {
		sql = sql[:2000] + "...[truncated]"
	}

	// 构建查询记录
	query := DBQuery{
		SQL:      sql,
		Duration: duration,
		Rows:     db.RowsAffected,
	}
	if db.Error != nil {
		query.Error = db.Error.Error()
	}

	// 添加到上下文
	if queries, exists := ginCtx.Get(dbQueriesKey); exists {
		if q, ok := queries.(*[]DBQuery); ok {
			*q = append(*q, query)
		}
	}
}
