package orm

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
)

var defaultDbPort = 3306
var defaultUseUnixSocket = false
var defaultMaxIdleConns = 100
var defaultMaxOpenConns = 100
var defaultConnMaxLifetimeMins = 15
var defaultMySqlLogger = logger.Discard.LogMode(logger.Silent) // rely on Opentelemetry
var defaultSQLiteLogger = logger.Default.LogMode(logger.Info)

type OrmConfig struct {
	DbName              string
	DbUser              string
	DbPassword          string
	DbHost              string
	DbPort              *int                  // defaults to 3306
	UseUnixSocket       *bool                 // defaults to false (set to true when on Cloud Run)
	MaxIdleConns        *int                  // default to 100
	MaxOpenConns        *int                  // default to 100
	ConnMaxLifetimeMins *int                  // defaults to 15
	MigrateModels       *func() []interface{} // Provide models to run migration for
	Logger              *logger.Interface
}

func (c *OrmConfig) setDefaults(
	defaultLogger logger.Interface,
) {
	if c.DbPort == nil {
		c.DbPort = &defaultDbPort
	}
	if c.UseUnixSocket == nil {
		c.UseUnixSocket = &defaultUseUnixSocket
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
	if c.Logger == nil {
		c.Logger = &defaultLogger
	}
}

type Orm struct {
	*gorm.DB
}

func NewMySqlOrm(config *OrmConfig) *Orm {
	config.setDefaults(
		defaultMySqlLogger,
	)

	db, err := gorm.Open(
		mysql.Open(dsn(config)),
		&gorm.Config{Logger: *config.Logger},
	)
	if err != nil {
		panic(err)
	}

	return newOrm(db, config)
}

func NewSQLiteOrm(config *OrmConfig) *Orm {
	config.setDefaults(
		defaultSQLiteLogger,
	)

	db, err := gorm.Open(
		sqlite.Open("file::memory:?cache=shared"),
		&gorm.Config{Logger: *config.Logger},
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

	if config.MigrateModels != nil {
		migrate(db, (*config.MigrateModels)()...)
	}

	return &Orm{db}
}

func migrate(db *gorm.DB, models ...interface{}) {
	err := db.AutoMigrate(models)
	if err != nil {
		panic("Migration failed: " + err.Error())
	}
}

// Create DB connection string based on the configuration given on creating the database object
func dsn(config *OrmConfig) string {
	// When running on Cloud Run we need to connect using Unix Sockets.
	// See https://cloud.google.com/sql/docs/mysql/connect-run#go
	if *config.UseUnixSocket {
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
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?parseTime=true",
		config.DbUser, config.DbPassword, config.DbHost, port, config.DbName)
}
