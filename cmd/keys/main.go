package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const keysVersion = "1.0.0"

type runtimeConfig struct {
	addr             string
	dataDir          string
	dbPath           string
	publicOrigin     string
	cookieSecure     bool
	adminPasswordRaw []byte
	masterKey        string
	sessionSecret    []byte
}

func main() {
	var (
		hashPassword = flag.Bool("hash-password", false, "read a password from stdin and print its bcrypt hash")
		checkConfig  = flag.Bool("check-config", false, "validate runtime configuration and exit")
		backupPath   = flag.String("backup", "", "write a consistent SQLite backup to this new path")
		version      = flag.Bool("version", false, "print version")
	)
	flag.Parse()

	if *version {
		fmt.Println("go-sites-keys", keysVersion)
		return
	}
	if *hashPassword {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
		if err != nil {
			log.Fatal("read password: ", err)
		}
		password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if password == "" || len(password) > 4096 {
			log.Fatal("password must contain 1 to 4096 bytes")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			log.Fatal("hash password: ", err)
		}
		fmt.Println(string(hash))
		return
	}

	config, err := loadRuntimeConfig(*backupPath == "")
	if err != nil {
		log.Fatal(err)
	}
	cipher, err := newSecretCipher(config.masterKey)
	if err != nil {
		log.Fatal(err)
	}
	store, err := openKeyStore(config.dbPath, cipher)
	if err != nil {
		log.Fatal(err)
	}
	defer store.close()

	if *backupPath != "" {
		if err := store.backup(*backupPath); err != nil {
			log.Fatal("backup database: ", err)
		}
		fmt.Println(*backupPath)
		return
	}
	if *checkConfig {
		if _, err := newAuthManager(config.adminPasswordRaw, config.sessionSecret, config.publicOrigin, config.cookieSecure); err != nil {
			log.Fatal(err)
		}
		fmt.Println("configuration ok")
		return
	}

	auth, err := newAuthManager(
		config.adminPasswordRaw, config.sessionSecret, config.publicOrigin, config.cookieSecure,
	)
	if err != nil {
		log.Fatal(err)
	}
	application, err := newApp(store, newDetector(), auth, config.publicOrigin)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              config.addr,
		Handler:           application.handler(),
		ReadHeaderTimeout: 8 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("keys service %s listening on %s", keysVersion, config.addr)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadRuntimeConfig(requireAuth bool) (runtimeConfig, error) {
	dataDir := envOr("KEYS_DATA_DIR", "./keys-data")
	dbPath := envOr("KEYS_DB_PATH", filepath.Join(dataDir, "keys.db"))
	publicOrigin := strings.TrimRight(envOr("KEYS_PUBLIC_ORIGIN", "https://liangz77.cn"), "/")
	parsedOrigin, err := url.Parse(publicOrigin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return runtimeConfig{}, errors.New("KEYS_PUBLIC_ORIGIN must be an absolute URL")
	}
	if parsedOrigin.Scheme != "https" && parsedOrigin.Scheme != "http" {
		return runtimeConfig{}, errors.New("KEYS_PUBLIC_ORIGIN must use http or https")
	}
	cookieSecure, err := strconv.ParseBool(envOr("KEYS_COOKIE_SECURE", "true"))
	if err != nil {
		return runtimeConfig{}, errors.New("KEYS_COOKIE_SECURE must be true or false")
	}
	if cookieSecure && parsedOrigin.Scheme != "https" {
		return runtimeConfig{}, errors.New("KEYS_PUBLIC_ORIGIN must use HTTPS when secure cookies are enabled")
	}
	masterKey, err := secretFromEnv("KEYS_MASTER_KEY", "KEYS_MASTER_KEY_FILE")
	if err != nil {
		return runtimeConfig{}, err
	}
	if strings.TrimSpace(masterKey) == "" {
		return runtimeConfig{}, errors.New("KEYS_MASTER_KEY or KEYS_MASTER_KEY_FILE is required")
	}
	config := runtimeConfig{
		addr:         envOr("KEYS_ADDR", "127.0.0.1:9020"),
		dataDir:      dataDir,
		dbPath:       dbPath,
		publicOrigin: publicOrigin,
		cookieSecure: cookieSecure,
		masterKey:    strings.TrimSpace(masterKey),
	}
	if !requireAuth {
		return config, nil
	}
	hash, err := secretFromEnv("KEYS_ADMIN_PASSWORD_HASH", "KEYS_ADMIN_PASSWORD_HASH_FILE")
	if err != nil {
		return runtimeConfig{}, err
	}
	if strings.TrimSpace(hash) == "" {
		return runtimeConfig{}, errors.New("KEYS_ADMIN_PASSWORD_HASH or KEYS_ADMIN_PASSWORD_HASH_FILE is required")
	}
	sessionSecret, err := secretFromEnv("KEYS_SESSION_SECRET", "KEYS_SESSION_SECRET_FILE")
	if err != nil {
		return runtimeConfig{}, err
	}
	if len(strings.TrimSpace(sessionSecret)) < 32 {
		return runtimeConfig{}, errors.New("KEYS_SESSION_SECRET must contain at least 32 characters")
	}
	config.adminPasswordRaw = []byte(strings.TrimSpace(hash))
	config.sessionSecret = []byte(strings.TrimSpace(sessionSecret))
	return config, nil
}

func secretFromEnv(inlineName, fileName string) (string, error) {
	if inline := strings.TrimSpace(os.Getenv(inlineName)); inline != "" {
		return inline, nil
	}
	path := strings.TrimSpace(os.Getenv(fileName))
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileName, err)
	}
	if len(data) > 32<<10 {
		return "", fmt.Errorf("%s is unexpectedly large", fileName)
	}
	return strings.TrimSpace(string(data)), nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
