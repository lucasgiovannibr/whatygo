package config

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gomessguii/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	config_env "github.com/lucasgiovannibr/whatygo/pkg/config/env"
)

type Config struct {
	PostgresAuthDB       string
	postgresUsersDB      string
	PostgresHost         string
	PostgresPort         string
	PostgresUser         string
	PostgresPassword     string
	PostgresDB           string
	DatabaseSaveMessages bool
	GlobalApiKey         string
	WaDebug              string
	LogType              string
	WebhookFiles         bool
	ConnectOnStartup     bool
	RerequestFromPhone   bool
	OsName               string
	AmqpUrl              string
	AmqpGlobalEnabled    bool
	WebhookUrl           string
	ClientName           string
	ApiAudioConverter    string
	ApiAudioConverterKey string
	MinioEndpoint        string
	MinioAccessKey       string
	MinioSecretKey       string
	MinioBucket          string
	MinioUseSSL          bool
	MinioEnabled         bool
	MinioRegion          string
	// MinioPublicBucket makes the bucket world-readable (MINIO_PUBLIC_BUCKET=true). Off by
	// default: it used to be applied silently, replacing the bucket's own policy.
	MinioPublicBucket bool
	// MinioURLTTL is how long the presigned media URLs work (MINIO_URL_TTL_HOURS, default
	// and maximum 168).
	MinioURLTTL          time.Duration
	WhatsappVersionMajor int
	WhatsappVersionMinor int
	WhatsappVersionPatch int
	ProxyProtocol        string
	ProxyHost            string
	ProxyFailClosed      bool
	PprofEnabled         bool
	// CorsOrigins are the browser origins allowed to call the API (CORS_ORIGINS, comma
	// separated). Empty or "*" allows every origin.
	CorsOrigins []string
	// TrustedProxies are the proxies (addresses or CIDRs, TRUSTED_PROXIES, comma separated)
	// whose X-Forwarded-For / X-Real-IP headers are believed. Empty, the default, trusts no
	// one: the client address is the connection's own. gin used to trust every sender, so any
	// client could make itself appear to come from any address.
	TrustedProxies []string
	// AuthFailLimit is how many failed authentications one client address may make per
	// minute before it is refused with 429 (AUTH_FAIL_LIMIT, default 30, 0 turns it off).
	AuthFailLimit int
	// MinTokenLength is the shortest token POST /instance/create accepts
	// (MIN_INSTANCE_TOKEN_LENGTH, default 16). The token is the credential of the instance.
	MinTokenLength int
	// MaxBodyBytes bounds a request body (MAX_BODY_MB, default 4); routes that receive a
	// file get MaxMediaBodyBytes (MAX_MEDIA_BODY_MB, default 150: 100 MB of media is
	// ~134 MB as base64).
	MaxBodyBytes      int64
	MaxMediaBodyBytes int64
	// WebhookIncludeToken keeps the instance API token ("instanceToken") in every event
	// payload sent to webhooks, queues and websockets. The token is the credential of the
	// instance, so every consumer of the events could use it: it is off unless
	// WEBHOOK_INCLUDE_TOKEN=true, for integrations that still read it.
	WebhookIncludeToken bool
	ProxyPort           string
	ProxyUsername       string
	ProxyPassword       string
	AmqpGlobalEvents    []string
	AmqpSpecificEvents  []string
	NatsUrl             string
	NatsGlobalEnabled   bool
	NatsGlobalEvents    []string
	EventIgnoreGroup    bool
	EventIgnoreStatus   bool
	QrcodeMaxCount      int
	CheckUserExists     bool
	// CheckUserCacheTTL is how long "this number is on WhatsApp" is remembered before a
	// send asks again (CHECK_USER_CACHE_TTL_MIN, minutes, default 720; 0 asks on every
	// send, as before). "Not registered" is remembered for 5 minutes only.
	CheckUserCacheTTL time.Duration

	// Calls (see pkg/call/engine). Zero means the engine default.
	CallMaxConcurrent int // calls one instance may have at the same time
	CallRingTimeout   int // seconds before an unanswered call is dropped
	// CallStreamGrace is how many seconds a running call waits for its audio stream to
	// come back before it is hung up.
	CallStreamGrace int
	// CallDialLimit is how many calls one instance may place per minute.
	CallDialLimit int
	// CallMediaStall is how many seconds an active call with a stream may receive no audio
	// from the peer before CallMediaStalled is published. Zero is the engine default, a
	// negative value turns the check off (CALL_MEDIA_STALL=0).
	CallMediaStall int
	// CallMediaStallHangup also hangs such a call up (CALL_MEDIA_STALL_HANGUP=true).
	CallMediaStallHangup bool
	// CallMaxDuration is how many seconds an answered call may last before it is hung up
	// (CALL_MAX_DURATION); zero, the default, is no limit.
	CallMaxDuration int
	// CallSilenceTimeout is how many seconds a call with a stream may go without a sound
	// from either side before it is hung up (CALL_SILENCE_TIMEOUT); zero never does.
	CallSilenceTimeout int

	// CallHistory keeps a record of every call in the database (CALL_HISTORY=true); off by
	// default, the peer's number is personal data.
	CallHistory bool
	// CallHistoryRetentionDays is how long those records are kept (CALL_HISTORY_RETENTION_DAYS,
	// default 90); zero keeps them for ever.
	CallHistoryRetentionDays int

	// CallStreamOrigins are the browser origins allowed to open the audio stream
	// besides the server's own; clients that send no Origin (servers, scripts) always may.
	CallStreamOrigins []string

	// Logger configurations
	LogMaxSize    int
	LogMaxBackups int
	LogMaxAge     int
	LogDirectory  string
	LogCompress   bool
	// LogLevel is the minimum severity written to the instance logs and the console
	// (LOG_LEVEL: debug, info, warn, error; default info). DEBUG lines used to be written
	// always.
	LogLevel string
	// LogKeepDeleted keeps the log directory of an instance after it is deleted. Off by
	// default: those logs hold the instance token, JIDs and message metadata.
	LogKeepDeleted bool
}

// AddInstanceToken puts the instance token in an event payload only when
// WEBHOOK_INCLUDE_TOKEN=true. A nil Config (tests, tools) never adds it.
func (c *Config) AddInstanceToken(payload map[string]interface{}, token string) {
	if c != nil && c.WebhookIncludeToken {
		payload["instanceToken"] = token
	}
}

// publicExampleKeys are GLOBAL_API_KEY values that ship in this repository's examples: a
// server started with one of them has a master key everyone can read on GitHub.
var publicExampleKeys = []string{
	"429683C4C977415CAAFCCE10F7D57E11",           // .env.example
	"sua-chave-api-segura-aqui",                  // docker/examples/*.yml
	"sua-chave-api-segura-aqui-uuid-recomendado", // docker/examples/.env.example
	"your-secure-api-key-here",                   // README.md
	"change-me",
	"changeme",
}

// minGlobalApiKeyLength is the length below which a master key is reported as weak.
const minGlobalApiKeyLength = 32

// CheckGlobalApiKey judges GLOBAL_API_KEY, the credential of every administrative route.
// A key from the repository's examples is an error (the caller refuses to start); a short
// one is only a warning.
func CheckGlobalApiKey(key string) (warning string, err error) {
	for _, known := range publicExampleKeys {
		if strings.EqualFold(key, known) {
			return "", fmt.Errorf("GLOBAL_API_KEY is a value published in the project's examples, anyone can use it as the master key: generate your own (for instance `openssl rand -hex 32`)")
		}
	}
	if len(key) < minGlobalApiKeyLength {
		return fmt.Sprintf("GLOBAL_API_KEY has %d characters; use at least %d (for instance `openssl rand -hex 32`)", len(key), minGlobalApiKeyLength), nil
	}
	return "", nil
}

// EnsureDBExists connects to postgres (without the target database) and creates it if it doesn't exist.
func (c *Config) EnsureDBExists(dsn string) error {
	return ensureDBExists(dsn)
}

// ensureDBExists connects to postgres (without the target database) and creates it if it doesn't exist.
func ensureDBExists(dsn string) error {
	dbName, adminDSN, err := extractDBNameAndAdminDSN(dsn)
	if err != nil {
		return err
	}

	db, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres for auto-setup: %v", err)
	}
	defer db.Close()

	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check database existence: %v", err)
	}

	if !exists {
		logger.LogInfo("[CONFIG] Database %q not found, creating it automatically...", dbName)
		_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %q", dbName))
		if err != nil {
			return fmt.Errorf("failed to create database %q: %v", dbName, err)
		}
		logger.LogInfo("[CONFIG] Database %q created successfully", dbName)
	}

	return nil
}

// extractDBNameAndAdminDSN parses a DSN (URL or key=value) and returns the database name
// and a DSN pointing to the "postgres" maintenance database.
func extractDBNameAndAdminDSN(dsn string) (string, string, error) {
	// Try URL format: postgres://user:pass@host:port/dbname?...
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", "", fmt.Errorf("failed to parse DSN URL: %v", err)
		}
		dbName := strings.TrimPrefix(u.Path, "/")
		u.Path = "/postgres"
		return dbName, u.String(), nil
	}

	// Key=value format: host=... user=... password=... dbname=... sslmode=...
	parts := strings.Fields(dsn)
	kvMap := make(map[string]string, len(parts))
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			kvMap[kv[0]] = kv[1]
		}
	}
	dbName, ok := kvMap["dbname"]
	if !ok || dbName == "" {
		return "", "", fmt.Errorf("could not extract dbname from DSN")
	}
	kvMap["dbname"] = "postgres"
	adminParts := make([]string, 0, len(kvMap))
	for k, v := range kvMap {
		adminParts = append(adminParts, k+"="+v)
	}
	return dbName, strings.Join(adminParts, " "), nil
}

func (c *Config) CreateUsersDB() (*gorm.DB, error) {
	logger.LogDebug("Connecting to the users database")

	dbDSN := c.postgresUsersDB

	if c.postgresUsersDB == "" {
		dbDSN = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", c.PostgresHost, c.PostgresPort, c.PostgresUser, c.PostgresPassword, c.PostgresDB)
	}

	if err := ensureDBExists(dbDSN); err != nil {
		logger.LogWarn("[CONFIG] Auto-setup failed (will try connecting anyway): %v", err)
	}

	db, err := gorm.Open(
		postgres.Open(dbDSN),
		&gorm.Config{},
	)
	if err != nil {
		return nil, err
	}

	// Configurar pool de conexões no GORM
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter sql.DB do GORM: %v", err)
	}

	// Pool sized by DB_MAX_OPEN_CONNS and friends
	DBPoolFromEnv().Apply(sqlDB)

	return db, nil
}

func (c *Config) CreateAuthDB() (*sql.DB, error) {
	dbDSN := c.postgresUsersDB

	if c.postgresUsersDB == "" {
		dbDSN = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", c.PostgresHost, c.PostgresPort, c.PostgresUser, c.PostgresPassword, c.PostgresDB)
	}

	if err := ensureDBExists(dbDSN); err != nil {
		logger.LogWarn("[CONFIG] Auto-setup failed (will try connecting anyway): %v", err)
	}

	db, err := sql.Open("postgres", dbDSN)
	if err != nil {
		return nil, err
	}

	// Pool sized by DB_MAX_OPEN_CONNS and friends
	DBPoolFromEnv().Apply(db)

	// Testar a conexão
	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("erro ao testar conexão PostgreSQL AUTH: %v", err)
	}

	return db, nil
}

func Load() *Config {
	postgresAuthDB := os.Getenv(config_env.POSTGRES_AUTH_DB)

	postgresUsersDB := os.Getenv(config_env.POSTGRES_USERS_DB)

	postgresHost := os.Getenv(config_env.POSTGRES_HOST)
	postgresPort := os.Getenv(config_env.POSTGRES_PORT)
	postgresUser := os.Getenv(config_env.POSTGRES_USER)
	postgresPassword := os.Getenv(config_env.POSTGRES_PASSWORD)
	postgresDB := os.Getenv(config_env.POSTGRES_DB)

	if postgresUsersDB == "" && (postgresHost == "" || postgresPort == "" || postgresUser == "" || postgresPassword == "" || postgresDB == "") {
		logger.LogFatal("[CONFIG] required database configuration variables are missing. Please check your environment configuration.")
	}

	databaseSaveMessages := os.Getenv(config_env.DATABASE_SAVE_MESSAGES)
	panicIfEmpty(config_env.DATABASE_SAVE_MESSAGES, databaseSaveMessages)

	globalApiKey := os.Getenv(config_env.GLOBAL_API_KEY)
	panicIfEmpty(config_env.GLOBAL_API_KEY, globalApiKey)
	if warning, err := CheckGlobalApiKey(globalApiKey); err != nil {
		if os.Getenv(config_env.ALLOW_INSECURE_API_KEY) != "true" {
			logger.LogFatal("[CONFIG] %v (set ALLOW_INSECURE_API_KEY=true to start anyway, for local development only)", err)
		}
		logger.LogWarn("[CONFIG] %v: starting anyway because ALLOW_INSECURE_API_KEY=true", err)
	} else if warning != "" {
		logger.LogWarn("[CONFIG] %s", warning)
	}

	clientName := os.Getenv(config_env.CLIENT_NAME)

	waDebug := getenvAny(config_env.WA_DEBUG, config_env.WA_DEBUG_LEGACY)

	logType := getenvAny(config_env.LOGTYPE, config_env.LOGTYPE_LEGACY)

	webhookFiles := os.Getenv(config_env.WEBHOOKFILES)
	if webhookFiles == "" {
		webhookFiles = "true"
	}

	connectOnStartup := os.Getenv(config_env.CONNECT_ON_STARTUP)
	if connectOnStartup == "" {
		connectOnStartup = "false"
	}

	osName := os.Getenv(config_env.OS_NAME)

	amqpUrl := os.Getenv(config_env.AMQP_URL)

	// Validate AMQP URL format
	if err := validateAMQPURL(amqpUrl); err != nil {
		logger.LogFatal("[CONFIG] AMQP URL validation failed: %v", err)
	}

	amqpGlobalEnabled := os.Getenv(config_env.AMQP_GLOBAL_ENABLED)

	webhookUrl := os.Getenv(config_env.WEBHOOK_URL)

	apiAudioConverter := os.Getenv(config_env.API_AUDIO_CONVERTER)
	apiAudioConverterKey := os.Getenv(config_env.API_AUDIO_CONVERTER_KEY)

	whatsappVersionMajor := os.Getenv(config_env.WHATSAPP_VERSION_MAJOR)
	whatsappVersionMinor := os.Getenv(config_env.WHATSAPP_VERSION_MINOR)
	whatsappVersionPatch := os.Getenv(config_env.WHATSAPP_VERSION_PATCH)

	proxyProtocol := os.Getenv(config_env.PROXY_PROTOCOL)
	proxyHost := os.Getenv(config_env.PROXY_HOST)
	proxyPort := os.Getenv(config_env.PROXY_PORT)
	proxyUsername := os.Getenv(config_env.PROXY_USERNAME)
	proxyPassword := os.Getenv(config_env.PROXY_PASSWORD)

	eventIgnoreGroup := os.Getenv(config_env.EVENT_IGNORE_GROUP)
	eventIgnoreStatus := os.Getenv(config_env.EVENT_IGNORE_STATUS)
	qrcodeMaxCount := os.Getenv(config_env.QRCODE_MAX_COUNT)
	checkUserExists := os.Getenv(config_env.CHECK_USER_EXISTS)

	if checkUserExists == "" {
		checkUserExists = "true"
	}

	rerequestFromPhone := os.Getenv(config_env.REREQUEST_FROM_PHONE)

	// Invalid or non-positive values fall back to the call engine defaults.
	callMaxConcurrent, _ := strconv.Atoi(os.Getenv(config_env.CALL_MAX_CONCURRENT))
	callRingTimeout, _ := strconv.Atoi(os.Getenv(config_env.CALL_RING_TIMEOUT))
	callStreamGrace, _ := strconv.Atoi(os.Getenv(config_env.CALL_STREAM_GRACE))
	callDialLimit, _ := strconv.Atoi(os.Getenv(config_env.CALL_DIAL_LIMIT))
	callMaxDuration, _ := strconv.Atoi(strings.TrimSpace(os.Getenv(config_env.CALL_MAX_DURATION)))
	callSilenceTimeout, _ := strconv.Atoi(strings.TrimSpace(os.Getenv(config_env.CALL_SILENCE_TIMEOUT)))
	var callStreamOrigins []string
	for _, origin := range strings.Split(os.Getenv(config_env.CALL_STREAM_ORIGINS), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			callStreamOrigins = append(callStreamOrigins, origin)
		}
	}

	// Convertendo para int com valores padrão caso estejam vazios
	major := 0
	if whatsappVersionMajor != "" {
		major, _ = strconv.Atoi(whatsappVersionMajor)
	}
	minor := 0
	if whatsappVersionMinor != "" {
		minor, _ = strconv.Atoi(whatsappVersionMinor)
	}
	patch := 0
	if whatsappVersionPatch != "" {
		patch, _ = strconv.Atoi(whatsappVersionPatch)
	}

	qrMaxCount := 5 // Valor padrão
	if qrcodeMaxCount != "" {
		qrMaxCount, _ = strconv.Atoi(qrcodeMaxCount)
	}

	amqpGlobalEvents := strings.Split(os.Getenv(config_env.AMQP_GLOBAL_EVENTS), ",")
	if len(amqpGlobalEvents) == 1 && amqpGlobalEvents[0] == "" {
		amqpGlobalEvents = []string{}
	}

	amqpSpecificEvents := strings.Split(os.Getenv(config_env.AMQP_SPECIFIC_EVENTS), ",")
	if len(amqpSpecificEvents) == 1 && amqpSpecificEvents[0] == "" {
		amqpSpecificEvents = []string{}
	}

	natsUrl := os.Getenv(config_env.NATS_URL)
	natsGlobalEnabled := os.Getenv(config_env.NATS_GLOBAL_ENABLED)
	natsGlobalEvents := strings.Split(os.Getenv(config_env.NATS_GLOBAL_EVENTS), ",")
	if len(natsGlobalEvents) == 1 && natsGlobalEvents[0] == "" {
		natsGlobalEvents = []string{}
	}

	// Logger configurations
	logMaxSize, _ := strconv.Atoi(os.Getenv(config_env.LOG_MAX_SIZE))
	if logMaxSize == 0 {
		logMaxSize = 100 // Default 100MB
	}

	logMaxBackups, _ := strconv.Atoi(os.Getenv(config_env.LOG_MAX_BACKUPS))
	if logMaxBackups == 0 {
		logMaxBackups = 5 // Default 5 backups
	}

	logMaxAge, _ := strconv.Atoi(os.Getenv(config_env.LOG_MAX_AGE))
	if logMaxAge == 0 {
		logMaxAge = 30 // Default 30 days
	}

	logDirectory := os.Getenv(config_env.LOG_DIRECTORY)
	if logDirectory == "" {
		logDirectory = "./logs" // Default logs directory
	}

	logCompress := os.Getenv(config_env.LOG_COMPRESS) == "true"
	if os.Getenv(config_env.LOG_COMPRESS) == "" {
		logCompress = true // Default compression enabled
	}

	var corsOrigins []string
	for _, origin := range strings.Split(os.Getenv(config_env.CORS_ORIGINS), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			corsOrigins = append(corsOrigins, origin)
		}
	}

	trustedProxies, err := parseTrustedProxies(os.Getenv(config_env.TRUSTED_PROXIES))
	if err != nil {
		logger.LogFatal("[CONFIG] TRUSTED_PROXIES: %v", err)
	}

	config := &Config{
		CorsOrigins:          corsOrigins,
		TrustedProxies:       trustedProxies,
		AuthFailLimit:        envNonNegative(config_env.AUTH_FAIL_LIMIT, 30),
		MinTokenLength:       envNonNegative(config_env.MIN_INSTANCE_TOKEN_LEN, 16),
		MaxBodyBytes:         envMB(config_env.MAX_BODY_MB, 4),
		MaxMediaBodyBytes:    envMB(config_env.MAX_MEDIA_BODY_MB, 150),
		PostgresAuthDB:       postgresAuthDB,
		postgresUsersDB:      postgresUsersDB,
		DatabaseSaveMessages: databaseSaveMessages == "true",
		GlobalApiKey:         globalApiKey,
		WaDebug:              waDebug,
		LogType:              logType,
		WebhookFiles:         webhookFiles == "true",
		ConnectOnStartup:     connectOnStartup == "true",
		OsName:               osName,
		AmqpUrl:              amqpUrl,
		AmqpGlobalEnabled:    amqpGlobalEnabled == "true",
		WebhookUrl:           webhookUrl,
		ClientName:           clientName,
		ApiAudioConverter:    apiAudioConverter,
		ApiAudioConverterKey: apiAudioConverterKey,
		PostgresHost:         postgresHost,
		PostgresPort:         postgresPort,
		PostgresUser:         postgresUser,
		PostgresPassword:     postgresPassword,
		PostgresDB:           postgresDB,
		WhatsappVersionMajor: major,
		WhatsappVersionMinor: minor,
		WhatsappVersionPatch: patch,
		ProxyProtocol:        proxyProtocol,
		ProxyHost:            proxyHost,
		ProxyFailClosed:      os.Getenv(config_env.PROXY_FAIL_CLOSED) == "true",
		PprofEnabled:         os.Getenv(config_env.ENABLE_PPROF) == "true",
		WebhookIncludeToken:  os.Getenv(config_env.WEBHOOK_INCLUDE_TOKEN) == "true",
		ProxyPort:            proxyPort,
		ProxyUsername:        proxyUsername,
		ProxyPassword:        proxyPassword,
		EventIgnoreGroup:     eventIgnoreGroup == "true",
		EventIgnoreStatus:    eventIgnoreStatus == "true",
		QrcodeMaxCount:       qrMaxCount,
		CallMaxConcurrent:    max(callMaxConcurrent, 0),
		CallRingTimeout:      max(callRingTimeout, 0),
		CallStreamGrace:      max(callStreamGrace, 0),
		CallDialLimit:        max(callDialLimit, 0),
		CallMaxDuration:      max(callMaxDuration, 0),
		CallSilenceTimeout:   max(callSilenceTimeout, 0),
		CallMediaStall:       callMediaStall(),

		CallHistory:              strings.EqualFold(strings.TrimSpace(os.Getenv(config_env.CALL_HISTORY)), "true"),
		CallHistoryRetentionDays: callHistoryRetentionDays(),

		CallMediaStallHangup: strings.EqualFold(strings.TrimSpace(os.Getenv(config_env.CALL_MEDIA_STALL_HANGUP)), "true"),
		CallStreamOrigins:    callStreamOrigins,
		CheckUserExists:      checkUserExists != "false", // Default true, set to false to disable
		CheckUserCacheTTL:    checkUserCacheTTL(),
		RerequestFromPhone:   rerequestFromPhone == "true",
		AmqpGlobalEvents:     amqpGlobalEvents,
		AmqpSpecificEvents:   amqpSpecificEvents,
		NatsUrl:              natsUrl,
		NatsGlobalEnabled:    natsGlobalEnabled == "true",
		NatsGlobalEvents:     natsGlobalEvents,
		LogMaxSize:           logMaxSize,
		LogMaxBackups:        logMaxBackups,
		LogMaxAge:            logMaxAge,
		LogDirectory:         logDirectory,
		LogCompress:          logCompress,
		LogKeepDeleted:       os.Getenv(config_env.LOG_KEEP_DELETED) == "true",
		LogLevel:             os.Getenv(config_env.LOG_LEVEL),
	}

	minioEnabled := os.Getenv(config_env.MINIO_ENABLED) == "true"
	if minioEnabled {
		config.MinioEnabled = true
		loadMinioConfig(config)
	}

	return config
}

// envMB reads a size in megabytes from the environment and returns it in bytes; an
// absent, invalid or non-positive value gives def.
// checkUserCacheTTL reads CHECK_USER_CACHE_TTL_MIN: minutes, default 720 (12 h); 0 turns
// the cache off; an invalid or negative value gives the default.
func checkUserCacheTTL() time.Duration {
	v := strings.TrimSpace(os.Getenv(config_env.CHECK_USER_CACHE_TTL_MIN))
	if v == "" {
		return 12 * time.Hour
	}
	minutes, err := strconv.Atoi(v)
	if err != nil || minutes < 0 {
		return 12 * time.Hour
	}
	return time.Duration(minutes) * time.Minute
}

// parseTrustedProxies reads a comma separated list of addresses and CIDRs.
func parseTrustedProxies(raw string) ([]string, error) {
	var out []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err != nil && net.ParseIP(entry) == nil {
			return nil, fmt.Errorf("%q is neither an IP address nor a CIDR", entry)
		}
		out = append(out, entry)
	}
	return out, nil
}

// envNonNegative reads a whole number >= 0 from the environment; absent or invalid gives def.
func envNonNegative(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v >= 0 {
		return v
	}
	return def
}

func envMB(name string, def int64) int64 {
	if v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64); err == nil && v > 0 {
		return v << 20
	}
	return def << 20
}

func loadMinioConfig(config *Config) {
	minioEndpoint := os.Getenv(config_env.MINIO_ENDPOINT)
	panicIfEmpty(config_env.MINIO_ENDPOINT, minioEndpoint)

	minioAccessKey := os.Getenv(config_env.MINIO_ACCESS_KEY)
	panicIfEmpty(config_env.MINIO_ACCESS_KEY, minioAccessKey)

	minioSecretKey := os.Getenv(config_env.MINIO_SECRET_KEY)
	panicIfEmpty(config_env.MINIO_SECRET_KEY, minioSecretKey)

	minioBucket := os.Getenv(config_env.MINIO_BUCKET)
	panicIfEmpty(config_env.MINIO_BUCKET, minioBucket)

	minioUseSSL := os.Getenv(config_env.MINIO_USE_SSL) == "true"

	minioRegion := os.Getenv(config_env.MINIO_REGION)

	config.MinioEndpoint = minioEndpoint
	config.MinioAccessKey = minioAccessKey
	config.MinioSecretKey = minioSecretKey
	config.MinioBucket = minioBucket
	config.MinioUseSSL = minioUseSSL
	config.MinioRegion = minioRegion
	config.MinioPublicBucket = os.Getenv(config_env.MINIO_PUBLIC_BUCKET) == "true"
	config.MinioURLTTL = 7 * 24 * time.Hour
	if hours, err := strconv.Atoi(strings.TrimSpace(os.Getenv(config_env.MINIO_URL_TTL_HOURS))); err == nil && hours > 0 && hours < 7*24 {
		config.MinioURLTTL = time.Duration(hours) * time.Hour
	}
}

// getenvAny returns the first of the named variables that is set (not empty).
func getenvAny(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

func panicIfEmpty(key, value string) {
	if value == "" {
		if os.Getenv("DEBUG_ENABLED") != "1" {
			logger.LogInfo("You are NOT on development mode")
		}
		logger.LogFatal("[CONFIG] required configuration variable is missing. Please check your environment configuration.")
	}
}

// validateAMQPURL validates if the AMQP URL has the correct scheme and format
func validateAMQPURL(amqpURL string) error {
	if amqpURL == "" {
		return nil // Empty URL is allowed (RabbitMQ disabled)
	}

	// Parse the URL
	parsedURL, err := url.Parse(amqpURL)
	if err != nil {
		return fmt.Errorf("invalid AMQP URL format: %v", err)
	}

	// Check if scheme is valid
	if parsedURL.Scheme != "amqp" && parsedURL.Scheme != "amqps" {
		return fmt.Errorf("AMQP scheme must be either 'amqp://' or 'amqps://', got: '%s://'", parsedURL.Scheme)
	}

	// Check if host is present
	if parsedURL.Host == "" {
		return fmt.Errorf("AMQP URL must include a host")
	}

	logger.LogInfo("[CONFIG] AMQP URL validation successful: %s://%s", parsedURL.Scheme, parsedURL.Host)
	return nil
}

// callMediaStall reads CALL_MEDIA_STALL. Unlike the other call settings 0 means "off"
// (reported as -1, since the engine reads 0 as "use the default"); unset or not a number
// keeps the default.
func callMediaStall() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(config_env.CALL_MEDIA_STALL)))
	switch {
	case err != nil:
		return 0
	case n <= 0:
		return -1
	}
	return n
}

// callHistoryRetentionDays reads CALL_HISTORY_RETENTION_DAYS: unset, negative or not a
// number is the default of 90 days, and 0 keeps the history for ever.
func callHistoryRetentionDays() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(config_env.CALL_HISTORY_RETENTION_DAYS)))
	if err != nil || n < 0 {
		return 90
	}
	return n
}
