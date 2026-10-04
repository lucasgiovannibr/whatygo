package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomessguii/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Level is the severity of a log line.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}

func (l Level) String() string { return levelNames[l] }

// ParseLevel reads LOG_LEVEL ("debug", "info", "warn"/"warning", "error"); anything else,
// including empty, is INFO.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	}
	return LevelInfo
}

// writeQueueSize is how many lines may wait for the disk, per instance. Entries are
// pointers, so an idle instance costs a few KB.
const writeQueueSize = 512

type LoggerManager struct {
	config  *config.Config
	level   Level
	loggers map[string]*Logger
	// released instances no longer get a file logger (see Release).
	released map[string]struct{}
	// discard is the shared console-only logger handed out for released instances.
	discard *Logger
	mu      sync.RWMutex
}

// Logger writes the log lines of one instance. A line is formatted by the caller, but its
// JSON encoding and the disk write happen in a goroutine of the logger: they used to run
// on the goroutine that logged, which is the one handling a WhatsApp event or an HTTP
// request (several lines per message, each a synchronous disk write under a mutex).
type Logger struct {
	config     *config.Config
	instanceId string
	level      Level
	writer     *lumberjack.Logger

	// queue carries the lines to the writer goroutine; nil for the console-only logger.
	queue chan *LogEntry
	done  chan struct{} // closed when the writer goroutine has finished
	// mu guards closed against sends on the closed queue.
	mu      sync.RWMutex
	closed  bool
	dropped atomic.Uint64 // lines lost because the queue was full
}

type LogEntry struct {
	Timestamp  time.Time       `json:"timestamp"`
	Level      string          `json:"level"`
	InstanceId string          `json:"instance_id"`
	Message    string          `json:"message"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`

	// flush, when set, makes the entry a marker: it is not written, and the channel is
	// closed once everything queued before it is on disk.
	flush chan struct{}
}

// managers are the live managers, so the process can flush them all when it stops.
var (
	managersMu sync.Mutex
	managers   []*LoggerManager
)

// CloseAll flushes and closes every logger of every manager. Call it when the process
// shuts down: lines still queued would otherwise be lost.
func CloseAll() {
	managersMu.Lock()
	all := append([]*LoggerManager(nil), managers...)
	managersMu.Unlock()
	for _, lm := range all {
		lm.Close()
	}
}

// Close flushes and closes the file logger of every instance.
func (lm *LoggerManager) Close() {
	lm.mu.Lock()
	loggers := make([]*Logger, 0, len(lm.loggers))
	for _, l := range lm.loggers {
		loggers = append(loggers, l)
	}
	lm.mu.Unlock()
	for _, l := range loggers {
		_ = l.Close()
	}
}

func NewLoggerManager(config *config.Config) *LoggerManager {
	// Garante que o diretório base de logs existe
	if err := os.MkdirAll(config.LogDirectory, 0755); err != nil {
		logger.LogError("Falha ao criar diretório base de logs: %v", err)
	}

	level := ParseLevel(config.LogLevel)
	lm := &LoggerManager{
		config:   config,
		level:    level,
		loggers:  make(map[string]*Logger),
		released: make(map[string]struct{}),
		discard:  &Logger{config: config, instanceId: "released", level: level},
	}
	managersMu.Lock()
	managers = append(managers, lm)
	managersMu.Unlock()
	return lm
}

// Release frees the file logger of an instance that no longer exists: it writes what is
// queued, closes the log file (one descriptor per instance was kept open for the life of
// the process) and forgets the logger. Later calls for that instance are still printed to
// the console but no longer create a file or a new logger.
//
// Known limit: the rotating-file library (lumberjack v2) starts a background
// goroutine per logger that it has no way to stop, so that one goroutine per
// instance ever created remains until the process exits.
func (lm *LoggerManager) Release(instanceId string) {
	lm.mu.Lock()
	l, ok := lm.loggers[instanceId]
	delete(lm.loggers, instanceId)
	lm.released[instanceId] = struct{}{}
	lm.mu.Unlock()

	if ok {
		_ = l.Close()
	}
}

// RemoveFiles deletes the log directory of an instance that was deleted (log files and
// rotated backups), unless LOG_KEEP_DELETED=true. They used to stay on disk forever, with
// the instance token, JIDs and message metadata in them. Call Release first, so the file
// is closed.
func (lm *LoggerManager) RemoveFiles(instanceId string) error {
	if lm.config.LogKeepDeleted {
		return nil
	}
	// The id names a directory below the log directory: never let it point elsewhere.
	if instanceId == "" || instanceId != filepath.Base(instanceId) || instanceId == "." || instanceId == ".." {
		return fmt.Errorf("invalid instance id %q for log removal", instanceId)
	}
	return os.RemoveAll(filepath.Join(lm.config.LogDirectory, instanceId))
}

// safeLoggerID reports whether id may name a log directory: letters, digits, dot, dash and
// underscore only, and not "." or "..". The id comes from the instances (UUIDs), from the
// process's own loggers ("system", the CLIENT_NAME) and, through the routes, from the URL:
// ".." used to write /app/instance.log outside the log directory, and any other string made
// a directory, a file and a goroutine that were never freed.
func safeLoggerID(id string) bool {
	if id == "" || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func (lm *LoggerManager) GetLogger(instanceId string) *Logger {
	if instanceId == "" {
		instanceId = "system" // no CLIENT_NAME: the process's own log
	}
	if !safeLoggerID(instanceId) {
		return lm.discard // console only, no file
	}

	lm.mu.RLock()
	logger, exists := lm.loggers[instanceId]
	_, isReleased := lm.released[instanceId]
	lm.mu.RUnlock()

	if exists {
		return logger
	}
	if isReleased {
		return lm.discard
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Verificar novamente após obter o lock de escrita
	if logger, exists = lm.loggers[instanceId]; exists {
		return logger
	}
	if _, isReleased = lm.released[instanceId]; isReleased {
		return lm.discard
	}

	// Criar novo logger para a instância
	logger = newLogger(instanceId, lm.config, lm.level)
	lm.loggers[instanceId] = logger
	return logger
}

func newLogger(instanceId string, config *config.Config, level Level) *Logger {
	// Garante que o diretório existe
	logPath := filepath.Join(config.LogDirectory, instanceId)
	os.MkdirAll(logPath, 0755)

	logFile := filepath.Join(logPath, "instance.log")

	writer := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    config.LogMaxSize,
		MaxBackups: config.LogMaxBackups,
		MaxAge:     config.LogMaxAge,
		Compress:   config.LogCompress,
	}

	l := &Logger{
		config:     config,
		instanceId: instanceId,
		level:      level,
		writer:     writer,
		queue:      make(chan *LogEntry, writeQueueSize),
		done:       make(chan struct{}),
	}
	go l.writeLoop()
	return l
}

func (l *Logger) LogInfo(format string, args ...interface{}) {
	l.logf(LevelInfo, format, args...)
}

func (l *Logger) LogError(format string, args ...interface{}) {
	l.logf(LevelError, format, args...)
}

func (l *Logger) LogWarn(format string, args ...interface{}) {
	l.logf(LevelWarn, format, args...)
}

func (l *Logger) LogDebug(format string, args ...interface{}) {
	l.logf(LevelDebug, format, args...)
}

// logf formats the line once (it used to be formatted twice, for the file and for the
// console), drops it when it is below the configured level, and queues it for the file.
func (l *Logger) logf(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}
	message := fmt.Sprintf(format, args...)

	l.enqueue(level, message)

	switch level {
	case LevelError:
		logger.LogError("%s", message)
	case LevelWarn:
		logger.LogWarn("%s", message)
	case LevelDebug:
		logger.LogDebug("%s", message)
	default:
		logger.LogInfo("%s", message)
	}
}

// enqueue hands a line to the writer. It never blocks: when the disk cannot keep up and
// the queue is full the line is dropped and counted (the writer reports the count).
func (l *Logger) enqueue(level Level, message string) {
	if l.queue == nil { // released instance: console only
		return
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return
	}
	select {
	case l.queue <- &LogEntry{Timestamp: time.Now(), Level: level.String(), InstanceId: l.instanceId, Message: message}:
	default:
		l.dropped.Add(1)
	}
}

// writeLoop is the only goroutine that touches the file.
func (l *Logger) writeLoop() {
	defer close(l.done)
	for entry := range l.queue {
		if entry.flush != nil {
			close(entry.flush)
			continue
		}
		l.write(entry)

		// Say so, in the log itself, when lines were lost.
		if n := l.dropped.Swap(0); n > 0 {
			l.write(&LogEntry{Timestamp: time.Now(), Level: LevelWarn.String(), InstanceId: l.instanceId,
				Message: fmt.Sprintf("%d log line(s) were dropped because the disk could not keep up", n)})
		}
	}
}

func (l *Logger) write(entry *LogEntry) {
	jsonEntry, err := json.Marshal(entry)
	if err != nil {
		logger.LogError("Failed to marshal log entry: %v", err)
		return
	}
	if _, err := l.writer.Write(append(jsonEntry, '\n')); err != nil {
		logger.LogError("Failed to write log: %v", err)
	}
}

// Flush returns when every line logged before the call is on disk.
func (l *Logger) Flush() {
	if l.queue == nil {
		return
	}
	marker := &LogEntry{flush: make(chan struct{})}

	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return
	}
	// Blocking, unlike enqueue: a flush must not be lost.
	l.queue <- marker
	l.mu.RUnlock()
	<-marker.flush
}

// Close writes what is queued, then closes the file.
func (l *Logger) Close() error {
	if l.queue == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		<-l.done
		return nil
	}
	l.closed = true
	close(l.queue)
	l.mu.Unlock()

	<-l.done
	return l.writer.Close()
}
