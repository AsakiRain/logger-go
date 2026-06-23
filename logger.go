package logger

import (
	"io"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config 定义了日志的配置项
type Config struct {
	ConsoleLevel string `toml:"console_level"` // e.g., "debug", "info"
	FileLevel    string `toml:"file_level"`    // e.g., "debug", "info"
	LogFile      string `toml:"log_file"`      // 日志文件路径
	MaxSize      int    `toml:"max_size"`      // 每个日志文件保存的最大尺寸 单位：MB (注意：不再是字节)
	MaxBackups   int    `toml:"max_backups"`   // 日志文件最多保存多少个备份
	MaxAge       int    `toml:"max_age"`       // 文件最多保存多少天 (0 表示不限制)
	Compress     bool   `toml:"compress"`      // 是否压缩旧日志
	DefaultTag   string `toml:"default_tag"`   // 默认 Tag
	ShowCaller   bool   `toml:"show_caller"`   // 是否在日志中显示调用者信息
}

// Logger 是我们封装的日志实例，通过匿名内嵌继承了 Zap 的所有方法
type Logger struct {
	*zap.SugaredLogger
	fileCloser io.Closer // 保存底层轮转器的句柄，用于释放文件锁
	showCaller bool      // 新增：保存状态供 WithTag 使用
}

// Default 全局默认实例，方便包级别直接调用（按需使用）
var Default *Logger

// InitDefault 初始化全局默认日志器
func InitDefault(cfg Config) {
	Default = New(cfg)
}

// New 创建一个新的日志实例
func New(cfg Config) *Logger {
	// 1. 解析日志级别
	consoleLevel, err := zapcore.ParseLevel(cfg.ConsoleLevel)
	if err != nil {
		consoleLevel = zapcore.InfoLevel
	}
	fileLevel, err := zapcore.ParseLevel(cfg.FileLevel)
	if err != nil {
		fileLevel = zapcore.DebugLevel
	}

	// ================= 2. 设置日志格式（Encoder）=================

	// ---> A. 控制台输出配置 (炫酷色彩版) <---
	consoleCfg := zap.NewProductionEncoderConfig()
	consoleCfg.ConsoleSeparator = " " // 保持紧凑

	// 1. 时间：青色 (Cyan)
	consoleCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("\x1b[36m[" + t.Format("2006-01-02 15:04:05") + "]\x1b[0m")
	}
	// 2. 级别：Zap 内置的高亮颜色
	consoleCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	// 3. 标签 (Name)：黄色 (Yellow)
	consoleCfg.EncodeName = func(name string, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("\x1b[33m[" + name + "]\x1b[0m")
	}
	// 4. 调用者：绿色 (Green)
	consoleCfg.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("\x1b[32m[" + caller.TrimmedPath() + "]\x1b[0m")
	}
	consoleEncoder := zapcore.NewConsoleEncoder(consoleCfg)

	// ---> B. 文件输出配置 (纯净机器可读版) <---
	fileCfg := zap.NewProductionEncoderConfig()
	fileCfg.ConsoleSeparator = " "

	// 1. 文件时间：无颜色，只加 []
	fileCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + t.Format("2006-01-02 15:04:05") + "]")
	}
	// 2. 文件级别：无颜色，强行加 [] 保持阵列整齐
	fileCfg.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + l.CapitalString() + "]")
	}
	// 3. 文件标签 (Name)：无颜色，只加 []
	fileCfg.EncodeName = func(name string, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + name + "]")
	}
	// 4. 文件调用者：无颜色，只加 []
	fileCfg.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + caller.TrimmedPath() + "]")
	}

	fileEncoder := zapcore.NewConsoleEncoder(fileCfg)

	// 3. 配置多个输出核心 (Core)
	var cores []zapcore.Core

	// -- 控制台输出 --
	cores = append(cores, zapcore.NewCore(
		consoleEncoder,
		zapcore.Lock(os.Stdout),
		consoleLevel,
	))

	// -- 文件轮转输出 --
	var rotator *lumberjack.Logger // 提前声明

	if cfg.LogFile != "" {
		// 设置 Lumberjack 默认值
		maxSize := cfg.MaxSize
		if maxSize <= 0 {
			maxSize = 100 // 默认 100MB
		}
		maxBackups := cfg.MaxBackups
		if maxBackups <= 0 {
			maxBackups = 5 // 默认 5 个备份
		}

		rotator = &lumberjack.Logger{
			Filename:   cfg.LogFile,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}

		cores = append(cores, zapcore.NewCore(
			fileEncoder,
			zapcore.AddSync(rotator),
			fileLevel,
		))
	}

	// 4. 合并 Core 并创建 Logger
	core := zapcore.NewTee(cores...)

	// 动态添加 Caller 选项
	var opts []zap.Option
	if cfg.ShowCaller {
		opts = append(opts, zap.AddCaller())
	}
	zLogger := zap.New(core, opts...)

	// 如果有默认 Tag，初始化时带上
	if cfg.DefaultTag != "" {
		zLogger = zLogger.Named(cfg.DefaultTag)
	}

	logger := &Logger{
		SugaredLogger: zLogger.Sugar(),
		showCaller:    cfg.ShowCaller, // 保存状态供 WithTag 使用
	}
	if rotator != nil {
		logger.fileCloser = rotator // 保存起来用于 Close
	}
	return logger
}

// WithTag 衍生出一个携带新 Tag 的 Logger 实例
func (l *Logger) WithTag(tag string) *Logger {
	var opts []zap.Option
	if l.showCaller {
		opts = append(opts, zap.AddCaller())
	}
	cleanLogger := zap.New(l.Desugar().Core(), opts...)

	return &Logger{
		SugaredLogger: cleanLogger.Named(tag).Sugar(),
		fileCloser:    l.fileCloser,
		showCaller:    l.showCaller, // 继承状态
	}
}

// Sync 替代了你之前的 Flush()，在程序退出前调用，确保日志全部刷入磁盘
func (l *Logger) Sync() error {
	return l.SugaredLogger.Sync()
}

func (l *Logger) Close() error {
	l.Sync()
	if l.fileCloser != nil {
		return l.fileCloser.Close()
	}
	return nil
}
