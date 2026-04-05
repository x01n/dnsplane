package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxFileSize = 10 * 1024 * 1024 // 10MB 单文件最大大小
	MaxBackups  = 30               // 最多保留文件数
	MaxAge      = 30               // 最多保留天数
)

// LogLevel 日志级别
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ANSI颜色码
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

// Logger 日志记录器（支持自动轮转）
type Logger struct {
	mu            sync.Mutex
	file          *os.File
	logger        *log.Logger
	currentDate   string
	logDir        string
	currentSize   int64
	level         LogLevel
	consoleOutput bool
	colorEnabled  bool
}

// New 创建新的日志记录器
func New() *Logger {
	return NewWithOptions("logs", LevelInfo, true, true)
}

// NewWithOptions 创建带选项的日志记录器
func NewWithOptions(logDir string, level LogLevel, consoleOutput, colorEnabled bool) *Logger {
	enableWindowsANSI()

	l := &Logger{
		logDir:        logDir,
		level:         level,
		consoleOutput: consoleOutput,
		colorEnabled:  colorEnabled,
	}

	// 创建logs目录
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
	}

	// 初始化日志文件
	if err := l.rotate(); err != nil {
		log.Printf("初始化日志文件失败: %v", err)
		l.logger = log.New(os.Stdout, "", 0)
		return l
	}

	// 启动后台轮转检查
	go l.rotateLoop()

	// 启动后台清理
	go l.cleanupLoop()

	return l
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetConsoleOutput 设置是否输出到控制台
func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enabled
}

// rotate 轮转日志文件
func (l *Logger) rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 关闭旧文件
	if l.file != nil {
		l.file.Close()
	}

	now := time.Now()
	dateStr := now.Format("2006-01-02")
	l.currentDate = dateStr
	filename := filepath.Join(l.logDir, dateStr+".log")
	if info, err := os.Stat(filename); err == nil && info.Size() >= MaxFileSize {
		// 直接删除超大文件
		os.Remove(filename)
	}

	// 打开/创建日志文件
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	l.file = file

	// 创建多输出writer
	var writers []io.Writer
	writers = append(writers, file)
	if l.consoleOutput {
		writers = append(writers, os.Stdout)
	}
	multiWriter := io.MultiWriter(writers...)
	l.logger = log.New(multiWriter, "", 0)

	// 获取当前文件大小
	if info, err := file.Stat(); err == nil {
		l.currentSize = info.Size()
	}

	return nil
}

// rotateLoop 后台轮转检查
func (l *Logger) rotateLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		dateStr := now.Format("2006-01-02")

		// 检查日期变化或文件大小超限
		l.mu.Lock()
		needRotate := l.currentDate != dateStr || l.currentSize >= MaxFileSize
		l.mu.Unlock()

		if needRotate {
			if err := l.rotate(); err != nil {
				log.Printf("日志轮转失败: %v", err)
			}
		}
	}
}

// cleanupLoop 后台清理过期日志
func (l *Logger) cleanupLoop() {
	// 启动时立即清理一次
	l.cleanup()

	// 每天清理一次
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}

// cleanup 清理过期日志文件
func (l *Logger) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -MaxAge)

	files, err := filepath.Glob(filepath.Join(l.logDir, "*.log*"))
	if err != nil {
		return
	}

	// 按修改时间排序
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var fileList []fileInfo

	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}

		// 删除过期文件
		if info.ModTime().Before(cutoff) {
			os.Remove(f)
			continue
		}

		fileList = append(fileList, fileInfo{path: f, modTime: info.ModTime()})
	}

	// 如果备份文件数量超过限制，删除最旧的
	if len(fileList) > MaxBackups {
		sort.Slice(fileList, func(i, j int) bool {
			return fileList[i].modTime.After(fileList[j].modTime)
		})

		for i := MaxBackups; i < len(fileList); i++ {
			os.Remove(fileList[i].path)
		}
	}
}

// write 写入日志（输出到控制台+文件）
func (l *Logger) write(logLevel LogLevel, levelStr, format string, v ...interface{}) {
	l.writeWithDepth(logLevel, levelStr, 4, true, format, v...)
}

// writeFileOnly 仅写入日志文件，不输出到控制台（用于HTTP请求日志等已有控制台输出的场景）
func (l *Logger) writeFileOnly(logLevel LogLevel, levelStr, format string, v ...interface{}) {
	l.writeWithDepth(logLevel, levelStr, 4, false, format, v...)
}

// writeWithDepth 核心日志写入（可配置调用深度和是否输出控制台）
func (l *Logger) writeWithDepth(logLevel LogLevel, levelStr string, callerDepth int, toConsole bool, format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.logger == nil || logLevel < l.level {
		return
	}

	now := time.Now()
	timeStr := now.Format("15:04:05")

	// 获取调用者信息
	_, file, line, ok := runtime.Caller(callerDepth)
	caller := ""
	if ok {
		shortFile := filepath.Base(file)
		caller = fmt.Sprintf("%s:%d", shortFile, line)
	}

	// 格式化消息
	userMsg := fmt.Sprintf(format, v...)

	// 构建带颜色的控制台输出
	var levelColor string
	switch logLevel {
	case LevelDebug:
		levelColor = colorGray
	case LevelInfo:
		levelColor = colorGreen
	case LevelWarn:
		levelColor = colorYellow
	case LevelError:
		levelColor = colorRed
	}

	// 文件日志格式（完整日期，无颜色）
	fileDateStr := now.Format("2006-01-02 15:04:05")
	fileMsg := fmt.Sprintf("%s [%s] [%s] %s", fileDateStr, levelStr, caller, userMsg)

	showConsole := toConsole && l.consoleOutput

	if showConsole && l.colorEnabled {
		// 控制台带颜色输出（简短时间）
		consoleMsg := fmt.Sprintf("%s%s%s %s%-5s%s %s%s",
			colorGray, timeStr, colorReset,
			levelColor, levelStr, colorReset,
			colorCyan, caller+colorReset+" "+userMsg)
		fmt.Fprintln(os.Stdout, consoleMsg)
		// 只写文件
		if l.file != nil {
			fmt.Fprintln(l.file, fileMsg)
		}
	} else if showConsole {
		// 统一输出到控制台+文件
		l.logger.Println(fileMsg)
	} else {
		// 仅文件
		if l.file != nil {
			fmt.Fprintln(l.file, fileMsg)
		}
	}

	// 更新文件大小估算
	l.currentSize += int64(len(fileMsg) + 1)
}

// Info 记录信息日志
func (l *Logger) Info(format string, v ...interface{}) {
	l.write(LevelInfo, "INFO", format, v...)
}

// Warn 记录警告日志
func (l *Logger) Warn(format string, v ...interface{}) {
	l.write(LevelWarn, "WARN", format, v...)
}

// Error 记录错误日志
func (l *Logger) Error(format string, v ...interface{}) {
	l.write(LevelError, "ERROR", format, v...)
}

// Debug 记录调试日志
func (l *Logger) Debug(format string, v ...interface{}) {
	l.write(LevelDebug, "DEBUG", format, v...)
}

// Infof 格式化信息日志（别名）
func (l *Logger) Infof(format string, v ...interface{}) {
	l.Info(format, v...)
}

// Warnf 格式化警告日志（别名）
func (l *Logger) Warnf(format string, v ...interface{}) {
	l.Warn(format, v...)
}

// Errorf 格式化错误日志（别名）
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.Error(format, v...)
}

// Debugf 格式化调试日志（别名）
func (l *Logger) Debugf(format string, v ...interface{}) {
	l.Debug(format, v...)
}

// Close 关闭日志文件
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// GetLogFiles 获取日志文件列表
func GetLogFiles() ([]LogFileInfo, error) {
	files, err := filepath.Glob(filepath.Join("logs", "*.log*"))
	if err != nil {
		return nil, err
	}

	var result []LogFileInfo
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		result = append(result, LogFileInfo{
			Name:       filepath.Base(f),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			Compressed: strings.HasSuffix(f, ".gz"),
		})
	}

	// 按时间倒序
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModTime.After(result[j].ModTime)
	})

	return result, nil
}

// LogFileInfo 日志文件信息
type LogFileInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	Compressed bool      `json:"compressed"`
}

// 创建全局日志实例
var globalLogger *Logger

func init() {
	globalLogger = New()
}

// ==================== 全局日志函数 ====================

func Info(format string, v ...interface{}) {
	globalLogger.Info(format, v...)
}

func Warn(format string, v ...interface{}) {
	globalLogger.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	globalLogger.Error(format, v...)
}

func Debug(format string, v ...interface{}) {
	globalLogger.Debug(format, v...)
}

// Fatal 致命错误日志（记录后退出程序）
func Fatal(format string, v ...interface{}) {
	globalLogger.Error("[FATAL] "+format, v...)
	os.Exit(1)
}

// ==================== 仅写文件的全局日志函数（不输出到控制台）====================
// 用于HTTP请求日志等已有自己控制台输出的场景

func FileInfo(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelInfo, "INFO", format, v...)
}

func FileWarn(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelWarn, "WARN", format, v...)
}

func FileError(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelError, "ERROR", format, v...)
}

// ==================== 工具函数 ====================

func Close() {
	globalLogger.Close()
}

// SetLevel 设置全局日志级别
func SetGlobalLevel(level LogLevel) {
	globalLogger.SetLevel(level)
}

// GetLogger 获取全局Logger实例
func GetLogger() *Logger {
	return globalLogger
}
