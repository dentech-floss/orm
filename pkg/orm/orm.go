package orm

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
)

var defaultDbPort = 3306
var defaultMaxIdleConns = 25
var defaultMaxOpenConns = 25
var defaultConnMaxLifetimeMins = 5
var defaultMySQLLogger = logger.Discard.LogMode(logger.Silent) // rely on Opentelemetry
var defaultSQLiteLogger = logger.Default.LogMode(logger.Info)

// tlsConfigCount makes the name of each TLS config registered with the MySQL driver unique.
var tlsConfigCount atomic.Int64

// OrmConfig - configuration structure for config values at ORM module
type OrmConfig struct {
	OnGCP               bool
	DbName              string
	DbUser              string
	DbPassword          string
	DbHost              string
	DbPort              *int // defaults to 3306
	MaxIdleConns        *int // default to 100
	MaxOpenConns        *int // default to 100
	ConnMaxLifetimeMins *int // defaults to 15
	// TLSCAFile is the path to a PEM file with the CA(s) to trust. If set, TCP connections
	// use TLS and verify the server's certificate and hostname. Unix socket connections
	// (OnGCP) ignore it. Defaults to the environment variable DB_TLS_CA_FILE.
	TLSCAFile string
	Logger    logger.Interface
}

func (c *OrmConfig) setDefaults(
	defaultLogger logger.Interface,
) {
	if c.DbPort == nil {
		c.DbPort = &defaultDbPort
	}
	if c.MaxIdleConns == nil {
		c.MaxIdleConns = &defaultMaxIdleConns
	}
	if c.MaxOpenConns == nil {
		c.MaxOpenConns = &defaultMaxOpenConns
	}
	if c.ConnMaxLifetimeMins == nil {
		c.ConnMaxLifetimeMins = &defaultConnMaxLifetimeMins
	}
	if c.TLSCAFile == "" {
		c.TLSCAFile = os.Getenv("DB_TLS_CA_FILE")
	}
	if c.Logger == nil {
		c.Logger = defaultLogger
	}
}

// Orm - main structure for orm object
type Orm struct {
	*gorm.DB
	config *OrmConfig
}

// NewMySqlOrm - creates a new Orm object with MySQL connection
func NewMySqlOrm(config *OrmConfig) *Orm {
	config.setDefaults(defaultMySQLLogger)

	db, err := gorm.Open(
		mysql.Open(dsn(config)),
		&gorm.Config{Logger: config.Logger},
	)
	if err != nil {
		panic(err)
	}

	return newOrm(db, config)
}

// NewSQLiteOrm - creates a new Orm object with SQLite connection
func NewSQLiteOrm(config *OrmConfig) *Orm {
	config.setDefaults(defaultSQLiteLogger)

	db, err := gorm.Open(
		sqlite.Open("file::memory:?cache=shared"),
		&gorm.Config{Logger: config.Logger},
	)
	if err != nil {
		panic(err)
	}

	return newOrm(db, config)
}

func newOrm(db *gorm.DB, config *OrmConfig) *Orm {

	// instrument GORM for tracing
	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		panic(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(err)
	}

	// Tweak the connection pool -> https://www.alexedwards.net/blog/configuring-sqldb
	sqlDB.SetMaxIdleConns(*config.MaxIdleConns)
	sqlDB.SetMaxOpenConns(*config.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(*config.ConnMaxLifetimeMins) * time.Minute)

	return &Orm{db, config}
}

// Create DB connection string based on the configuration given on creating the database object
func dsn(config *OrmConfig) string {
	// When running on Cloud Run we need to connect using Unix Sockets.
	// See https://cloud.google.com/sql/docs/mysql/connect-run#go
	if config.OnGCP {
		return unixDsn(config)
	}

	return tcpDsn(config)
}

func unixDsn(config *OrmConfig) string {
	socketDir, isSet := os.LookupEnv("DB_SOCKET_DIR")
	if !isSet {
		socketDir = "cloudsql"
	}
	return fmt.Sprintf(
		"%s:%s@unix(/%s/%s)/%s?charset=utf8mb4&parseTime=true",
		config.DbUser, config.DbPassword, socketDir, config.DbHost, config.DbName)

}

func tcpDsn(config *OrmConfig) string {
	port := strconv.Itoa(*config.DbPort)
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true",
		config.DbUser, config.DbPassword, config.DbHost, port, config.DbName)

	if config.TLSCAFile != "" {
		dsn += "&tls=" + registerTLSConfig(config)
	}
	return dsn
}

// registerTLSConfig registers a TLS config that trusts the CA(s) in config.TLSCAFile and
// returns its name. The file can hold several certificates, so that the old and the new
// CA can both be trusted while a CA is replaced. It's read once, at startup.
//
// Panics if the file can't be used: falling back to an unencrypted connection, or to one
// that doesn't verify the server, must never happen silently.
func registerTLSConfig(config *OrmConfig) string {
	pem, err := os.ReadFile(config.TLSCAFile)
	if err != nil {
		panic(fmt.Errorf("orm: read TLS CA file: %w", err))
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(pem) {
		panic(fmt.Errorf("orm: no certificates in TLS CA file %s", config.TLSCAFile))
	}

	name := fmt.Sprintf("orm-%d", tlsConfigCount.Add(1))
	err = gomysql.RegisterTLSConfig(name, &tls.Config{
		RootCAs:    rootCAs,
		ServerName: config.DbHost,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		panic(fmt.Errorf("orm: register TLS config: %w", err))
	}
	return name
}
