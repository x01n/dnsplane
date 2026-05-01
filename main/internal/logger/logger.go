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

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

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

func New() *Logger {
	return NewWithOptions("logs", LevelInfo, true, true)
}

func NewWithOptions(logDir string, level LogLevel, consoleOutput, colorEnabled bool) *Logger {
	enableWindowsANSI()

	l := &Logger{
		logDir:        logDir,
		level:         level,
		consoleOutput: consoleOutput,
		colorEnabled:  colorEnabled,
	}
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
	}
	if err := l.rotate(); err != nil {
		log.Printf("初始化日志文件失败: %v", err)
		l.logger = log.New(os.Stdout, "", 0)
		return l
	}
	go l.rotateLoop()
	go l.cleanupLoop()

	return l
}

func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enabled
}

func (l *Logger) rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}

	now := time.Now()
	dateStr := now.Format("2006-01-02")
	l.currentDate = dateStr
	filename := filepath.Join(l.logDir, dateStr+".log")
	if info, err := os.Stat(filename); err == nil && info.Size() >= MaxFileSize {
		os.Remove(filename)
	}

	// 打开/创建日志文件
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	l.file = file
	var writers []io.Writer
	writers = append(writers, file)
	if l.consoleOutput {
		writers = append(writers, os.Stdout)
	}
	multiWriter := io.MultiWriter(writers...)
	l.logger = log.New(multiWriter, "", 0)
	if info, err := file.Stat(); err == nil {
		l.currentSize = info.Size()
	}

	return nil
}

func (l *Logger) rotateLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		dateStr := now.Format("2006-01-02")
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

func (l *Logger) cleanupLoop() {
	l.cleanup()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}
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
		if info.ModTime().Before(cutoff) {
			os.Remove(f)
			continue
		}

		fileList = append(fileList, fileInfo{path: f, modTime: info.ModTime()})
	}
	if len(fileList) > MaxBackups {
		sort.Slice(fileList, func(i, j int) bool {
			return fileList[i].modTime.After(fileList[j].modTime)
		})

		for i := MaxBackups; i < len(fileList); i++ {
			os.Remove(fileList[i].path)
		}
	}
}

func (l *Logger) write(logLevel LogLevel, levelStr, format string, v ...interface{}) {
	l.writeWithDepth(logLevel, levelStr, 4, true, format, v...)
}

func (l *Logger) writeFileOnly(logLevel LogLevel, levelStr, format string, v ...interface{}) {
	l.writeWithDepth(logLevel, levelStr, 4, false, format, v...)
}

func (l *Logger) writeWithDepth(logLevel LogLevel, levelStr string, callerDepth int, toConsole bool, format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.logger == nil || logLevel < l.level {
		return
	}
	now := time.Now()
	timeStr := now.Format("15:04:05")
	_, file, line, ok := runtime.Caller(callerDepth)
	caller := ""
	if ok {
		shortFile := filepath.Base(file)
		caller = fmt.Sprintf("%s:%d", shortFile, line)
	}
	userMsg := fmt.Sprintf(format, v...)
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
	fileDateStr := now.Format("2006-01-02 15:04:05")
	fileMsg := fmt.Sprintf("%s [%s] [%s] %s", fileDateStr, levelStr, caller, userMsg)

	showConsole := toConsole && l.consoleOutput

	if showConsole && l.colorEnabled {
		consoleMsg := fmt.Sprintf("%s%s%s %s%-5s%s %s%s",
			colorGray, timeStr, colorReset,
			levelColor, levelStr, colorReset,
			colorCyan, caller+colorReset+" "+userMsg)
		fmt.Fprintln(os.Stdout, consoleMsg)
		if l.file != nil {
			fmt.Fprintln(l.file, fileMsg)
		}
	} else if showConsole {
		l.logger.Println(fileMsg)
	} else {
		if l.file != nil {
			fmt.Fprintln(l.file, fileMsg)
		}
	}
	l.currentSize += int64(len(fileMsg) + 1)
}

func (l *Logger) Info(format string, v ...interface{}) {
	l.write(LevelInfo, "INFO", format, v...)
}

func (l *Logger) Warn(format string, v ...interface{}) {
	l.write(LevelWarn, "WARN", format, v...)
}

func (l *Logger) Error(format string, v ...interface{}) {
	l.write(LevelError, "ERROR", format, v...)
}

func (l *Logger) Debug(format string, v ...interface{}) {
	l.write(LevelDebug, "DEBUG", format, v...)
}

func (l *Logger) Infof(format string, v ...interface{}) {
	l.Info(format, v...)
}

func (l *Logger) Warnf(format string, v ...interface{}) {
	l.Warn(format, v...)
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	l.Error(format, v...)
}

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

func Fatal(format string, v ...interface{}) {
	globalLogger.Error("[FATAL] "+format, v...)
	os.Exit(1)
}

func FileInfo(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelInfo, "INFO", format, v...)
}

func FileWarn(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelWarn, "WARN", format, v...)
}

func FileError(format string, v ...interface{}) {
	globalLogger.writeFileOnly(LevelError, "ERROR", format, v...)
}


func Close() {
	globalLogger.Close()
}

func SetGlobalLevel(level LogLevel) {
	globalLogger.SetLevel(level)
}

func GetLogger() *Logger {
	return globalLogger
}
