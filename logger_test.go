package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap/zapcore"
)

type countingSink struct {
	bytes.Buffer
	syncs int
}

func (s *countingSink) Sync() error {
	s.syncs++
	return nil
}

// TestLoggerBasicAndTag 测试基础日志写入与 Tag 功能
// 覆盖所有可安全调用的日志级别: Debug, Info, Warn, Error, DPanic
func TestLoggerBasicAndTag(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	cfg := Config{
		ConsoleLevel: "debug",
		FileLevel:    "debug",
		LogFile:      logFile,
		DefaultTag:   "MAIN",
	}

	logger := New(cfg)
	defer logger.Close()

	// 测试所有可安全调用的日志级别
	logger.Debugf("debug message")
	logger.Infof("user %s logged in", "alice")
	logger.Warnf("connection lost")
	logger.Errorf("error occurred: %d", 500)
	logger.DPanic("dpanic message - development only")

	subLogger := logger.WithTag("UAPI")
	subLogger.Warnf("sub logger warn")

	// 确保刷入磁盘
	logger.Sync()

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}

	output := string(content)

	// 验证各级别日志内容
	tests := []struct {
		name    string
		content string
	}{
		{"Debug", "debug message"},
		{"Info", "user alice logged in"},
		{"Warn", "connection lost"},
		{"Error", "error occurred: 500"},
		{"DPanic", "dpanic message"},
		{"SubLogger", "sub logger warn"},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.content) {
			t.Errorf("%s 级别日志未正确写入", tt.name)
		}
	}

	// 验证级别标签
	levelTags := []string{"[DEBUG]", "[INFO]", "[WARN]", "[ERROR]"}
	for _, tag := range levelTags {
		if !strings.Contains(output, tag) {
			t.Errorf("日志级别标签 %s 未正确输出", tag)
		}
	}

	// 验证 Tag
	if !strings.Contains(output, `[MAIN]`) {
		t.Error("默认 Tag MAIN 未正确输出")
	}
	if !strings.Contains(output, `[UAPI]`) {
		t.Error("WithTag 衍生 logger 失败")
	}
}

// TestLoggerLevelFiltering 测试不同级别的过滤效果
// 合并了原有的 TestLoggerLevelFilter 功能
func TestLoggerLevelFiltering(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name        string
		fileLevel   string
		expectDebug bool
		expectInfo  bool
		expectWarn  bool
		expectError bool
	}{
		{
			name:        "debug level - all logs",
			fileLevel:   "debug",
			expectDebug: true,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "info level - no debug",
			fileLevel:   "info",
			expectDebug: false,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "warn level - only warn and above",
			fileLevel:   "warn",
			expectDebug: false,
			expectInfo:  false,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "error level - only error",
			fileLevel:   "error",
			expectDebug: false,
			expectInfo:  false,
			expectWarn:  false,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logFile := filepath.Join(tmpDir, strings.ReplaceAll(tc.name, " ", "_")+".log")

			cfg := Config{
				ConsoleLevel: "error", // 减少控制台输出
				FileLevel:    tc.fileLevel,
				LogFile:      logFile,
			}

			logger := New(cfg)
			defer logger.Close()

			logger.Debug("debug log")
			logger.Info("info log")
			logger.Warn("warn log")
			logger.Error("error log")
			logger.Sync()

			content, _ := os.ReadFile(logFile)
			output := string(content)

			checkLevel := func(level string, expected bool) {
				contains := strings.Contains(output, level+" log")
				if expected && !contains {
					t.Errorf("期望包含 %s 日志，但未找到", level)
				}
				if !expected && contains {
					t.Errorf("不应包含 %s 日志，但找到了", level)
				}
			}

			checkLevel("debug", tc.expectDebug)
			checkLevel("info", tc.expectInfo)
			checkLevel("warn", tc.expectWarn)
			checkLevel("error", tc.expectError)
		})
	}
}

func TestLoggerCloseWithoutLogFile(t *testing.T) {
	logger := New(Config{
		ConsoleLevel: "error",
		FileLevel:    "debug",
	})

	if err := logger.Close(); err != nil {
		t.Fatalf("Close without LogFile should not fail: %v", err)
	}
}

func TestFlushWriteSyncerSyncsEveryWrite(t *testing.T) {
	sink := &countingSink{}
	writer := maybeFlushWriter(sink, true)

	if _, err := writer.Write([]byte("first\n")); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if _, err := writer.Write([]byte("second\n")); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	if sink.syncs != 2 {
		t.Fatalf("sync count = %d, want 2", sink.syncs)
	}
	if got := sink.String(); got != "first\nsecond\n" {
		t.Fatalf("written content = %q", got)
	}
}

func TestFlushWriteSyncerCanBeDisabled(t *testing.T) {
	sink := &countingSink{}
	writer := maybeFlushWriter(sink, false)

	if _, err := writer.Write([]byte("message\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if sink.syncs != 0 {
		t.Fatalf("sync count = %d, want 0", sink.syncs)
	}
}

func TestLoggerFlushConfigWritesFileImmediately(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "flush.log")

	logger := New(Config{
		ConsoleLevel: "error",
		FileLevel:    "info",
		LogFile:      logFile,
		Flush:        true,
	})
	defer logger.Close()

	logger.Info("flush config message")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), "flush config message") {
		t.Fatalf("log file does not contain flushed message: %q", string(content))
	}
}

// TestLoggerConcurrent 测试高并发下的协程安全性
func TestLoggerConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrent.log")

	cfg := Config{
		ConsoleLevel: "error", // 减少控制台刷屏
		FileLevel:    "info",
		LogFile:      logFile,
	}

	logger := New(cfg)
	defer logger.Close()

	var wg sync.WaitGroup

	goroutineCount := 20
	logsPerGoroutine := 100

	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			gLogger := logger.WithTag("GOROUTINE")
			for j := 0; j < logsPerGoroutine; j++ {
				gLogger.Infof("message %d from goroutine %d", j, id)
			}
		}(i)
	}

	wg.Wait()
	logger.Sync()

	content, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	expectedLines := goroutineCount * logsPerGoroutine
	if len(lines) < expectedLines {
		t.Errorf("并发日志丢失: 预期 %d 行，实际 %d 行", expectedLines, len(lines))
	}
}

// TestLoggerRotate 测试 lumberjack 日志轮转功能
func TestLoggerRotate(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "rotate.log")

	cfg := Config{
		ConsoleLevel: "error",
		FileLevel:    "info",
		LogFile:      logFile,
		MaxSize:      1,
		MaxBackups:   3,
	}

	logger := New(cfg)
	defer logger.Close()

	// 构造一个约 100KB 的大字符串
	bigStr := strings.Repeat("A", 100*1024)

	// 写入 15 次，约 1.5MB，足以触发 1MB 的轮转
	for i := 0; i < 15; i++ {
		logger.Infof("filler data: %s", bigStr)
	}

	logger.Sync()

	// 检查目录下是否有轮转生成的备份文件 (lumberjack 的命名规则通常带有时间戳)
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}

	logFileCount := 0
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "rotate") && strings.Contains(f.Name(), ".log") {
			logFileCount++
		}
	}

	if logFileCount < 2 {
		t.Errorf("日志轮转未触发，当前目录下的日志文件数: %d", logFileCount)
	}
}

// TestLoggerShowCallerConfig 测试 ShowCaller 配置项是否生效及继承机制
func TestLoggerShowCallerConfig(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("ShowCaller=true", func(t *testing.T) {
		logFile := filepath.Join(tmpDir, "caller_true.log")
		cfg := Config{
			ConsoleLevel: "info",
			FileLevel:    "info",
			LogFile:      logFile,
			ShowCaller:   true,
		}

		logger := New(cfg)
		defer logger.Close()

		logger.Infof("this log should have caller info")
		logger.Sync()

		content, _ := os.ReadFile(logFile)
		output := string(content)

		// 检查输出中是否包含源文件位置，例如 [logger_test.go:xxx]
		if !strings.Contains(output, "logger_test.go:") {
			t.Errorf("配置了 ShowCaller=true，预期输出包含 caller 信息，但实际没有。\n实际输出: %s", output)
		}
	})

	t.Run("ShowCaller=false", func(t *testing.T) {
		logFile := filepath.Join(tmpDir, "caller_false.log")
		cfg := Config{
			ConsoleLevel: "info",
			FileLevel:    "info",
			LogFile:      logFile,
			ShowCaller:   false,
		}

		logger := New(cfg)
		defer logger.Close()

		logger.Infof("this log should NOT have caller info")
		logger.Sync()

		content, _ := os.ReadFile(logFile)
		output := string(content)

		// 检查输出中是否没有了源文件位置
		if strings.Contains(output, "logger_test.go:") {
			t.Errorf("配置了 ShowCaller=false，预期输出不应包含 caller 信息，但实际包含了。\n实际输出: %s", output)
		}
	})

	t.Run("WithTag Inherits ShowCaller", func(t *testing.T) {
		logFile := filepath.Join(tmpDir, "caller_tag_false.log")
		cfg := Config{
			ConsoleLevel: "info",
			FileLevel:    "info",
			LogFile:      logFile,
			ShowCaller:   false, // 根配置关闭
		}

		// 测试 WithTag 衍生出的新 Logger 是否继承了 false
		logger := New(cfg).WithTag("TEST_TAG")
		defer logger.Close()

		logger.Infof("tagged log should NOT have caller info either")
		logger.Sync()

		content, _ := os.ReadFile(logFile)
		output := string(content)

		if strings.Contains(output, "logger_test.go:") {
			t.Errorf("衍生 Logger 未正确继承 ShowCaller=false 的属性。\n实际输出: %s", output)
		}
		if !strings.Contains(output, "[TEST_TAG]") {
			t.Errorf("Tag 写入失败。\n实际输出: %s", output)
		}
	})
}

// TestDefaultLogger 测试包级别 Default 实例
func TestDefaultLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "default.log")

	InitDefault(Config{
		ConsoleLevel: "debug",
		FileLevel:    "debug",
		LogFile:      logFile,
		DefaultTag:   "GLOBAL",
	})
	defer Default.Close()

	Default.Infof("global logger test")
	Default.Sync()

	content, _ := os.ReadFile(logFile)
	if !strings.Contains(string(content), "global logger test") {
		t.Error("Default logger 写入失败")
	}
}

// TestLoggerPanicAndFatal 测试 Panic 和 Fatal 级别
// 注意：这些级别会导致程序终止，因此实际调用代码已注释
func TestLoggerPanicAndFatal(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "panic_fatal.log")

	cfg := Config{
		ConsoleLevel: "debug",
		FileLevel:    "debug",
		LogFile:      logFile,
		DefaultTag:   "PANIC_FATAL_TEST",
	}

	logger := New(cfg)
	defer logger.Close()

	// 以下级别会导致程序终止，取消注释可手动测试：
	// logger.Panic("this will panic")
	// logger.Panicf("this will panic: %s", "formatted")
	// logger.Fatal("this will exit")
	// logger.Fatalf("this will exit: %d", 1)

	t.Log("Panic 和 Fatal 级别测试已跳过（会导致程序终止），如需测试请取消注释相关代码")
}

// TestLoggerLevelConstants 验证 zap 支持的日志级别常量
// zap 支持的级别（从低到高）:
//
//	DebugLevel (-1): 调试信息
//	InfoLevel (0): 一般信息
//	WarnLevel (1): 警告信息
//	ErrorLevel (2): 错误信息
//	DPanicLevel (3): 开发环境 panic（生产环境仅记录日志）
//	PanicLevel (4): 记录日志后触发 panic
//	FatalLevel (5): 记录日志后退出程序
func TestLoggerLevelConstants(t *testing.T) {
	levels := []struct {
		name  string
		level zapcore.Level
	}{
		{"DebugLevel", zapcore.DebugLevel},   // -1
		{"InfoLevel", zapcore.InfoLevel},     // 0
		{"WarnLevel", zapcore.WarnLevel},     // 1
		{"ErrorLevel", zapcore.ErrorLevel},   // 2
		{"DPanicLevel", zapcore.DPanicLevel}, // 3
		{"PanicLevel", zapcore.PanicLevel},   // 4
		{"FatalLevel", zapcore.FatalLevel},   // 5
	}

	// 验证级别顺序
	for i := 0; i < len(levels)-1; i++ {
		if levels[i].level >= levels[i+1].level {
			t.Errorf("日志级别顺序错误: %s (%d) 应该小于 %s (%d)",
				levels[i].name, levels[i].level,
				levels[i+1].name, levels[i+1].level)
		}
	}

	// 验证具体值
	expectedValues := map[string]int8{
		"DebugLevel":  -1,
		"InfoLevel":   0,
		"WarnLevel":   1,
		"ErrorLevel":  2,
		"DPanicLevel": 3,
		"PanicLevel":  4,
		"FatalLevel":  5,
	}

	for _, l := range levels {
		if int8(l.level) != expectedValues[l.name] {
			t.Errorf("%s 的值错误: 期望 %d, 实际 %d",
				l.name, expectedValues[l.name], l.level)
		}
	}

	t.Logf("zap 支持的日志级别: Debug(-1) < Info(0) < Warn(1) < Error(2) < DPanic(3) < Panic(4) < Fatal(5)")
}
