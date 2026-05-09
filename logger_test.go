package logger

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestLoggerBasicAndTag 测试基础日志写入与 Tag 功能
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

	logger.Infof("user %s logged in", "alice")
	logger.Debugf("debug message")

	subLogger := logger.WithTag("UAPI")
	subLogger.Warnf("connection lost")

	// 确保刷入磁盘
	logger.Sync()

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}

	output := string(content)

	// 验证内容
	if !strings.Contains(output, "user alice logged in") {
		t.Error("Infof 格式化失败")
	}
	if !strings.Contains(output, "debug message") {
		t.Error("Debugf 写入失败")
	}
	if !strings.Contains(output, "connection lost") {
		t.Error("Warnf 写入失败")
	}

	// 验证 Tag
	if !strings.Contains(output, `[MAIN]`) {
		t.Error("默认 Tag MAIN 未正确输出")
	}
	if !strings.Contains(output, `[UAPI]`) {
		t.Error("WithTag 衍生 logger 失败")
	}
}

// TestLoggerLevelFilter 测试文件级别的过滤
func TestLoggerLevelFilter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "level.log")

	cfg := Config{
		ConsoleLevel: "info",
		FileLevel:    "warn", // 文件仅记录 warn 及以上
		LogFile:      logFile,
	}

	logger := New(cfg)
	defer logger.Close()

	logger.Infof("this should not be in file")
	logger.Warnf("this should be in file")
	logger.Sync()

	content, _ := os.ReadFile(logFile)
	output := string(content)

	if strings.Contains(output, "this should not be in file") {
		t.Error("Info 级别未被正确过滤")
	}
	if !strings.Contains(output, "this should be in file") {
		t.Error("Warn 级别写入失败")
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
