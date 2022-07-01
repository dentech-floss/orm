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

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
)

var defaultDbPort = 3306
var defaultMaxIdleConns = 100
var defaultMaxOpenConns = 100
var defaultConnMaxLifetimeMins = 15
var defaultMySqlLogger = logger.Discard.LogMode(logger.Silent) // rely on Opentelemetry
var defaultSQLiteLogger = logger.Default.LogMode(logger.Info)

type OrmConfig struct {
	OnGCP                 bool
	DbName                string
	DbUser                string
	DbPassword            string
	DbHost                string
	DbPort                *int // defaults to 3306
	MaxIdleConns          *int // default to 100
	MaxOpenConns          *int // default to 100
	ConnMaxLifetimeMins   *int // defaults to 15
	Logger                *logger.Interface
	MigrateUseTransaction bool
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
	if c.Logger == nil {
		c.Logger = &defaultLogger
	}
}

type Orm struct {
	*gorm.DB
	config *OrmConfig
}

// Migration represents a database migration (a modification to be made on the database).
type Migration struct {
	// ID is the migration identifier. Usually a timestamp like "201601021504".
	ID string
	// Migrate is a function that will br executed while running this migration.
	Migrate gormigrate.MigrateFunc
	// Rollback will be executed on rollback. Can be nil.
	Rollback gormigrate.RollbackFunc
}

func NewMySqlOrm(config *OrmConfig) *Orm {
	config.setDefaults(defaultMySqlLogger)

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
	config.setDefaults(defaultSQLiteLogger)

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

	return &Orm{db, config}
}

// Create DB connection string based on the configuration given on creating the database object
func dsn(config *OrmConfig) string {
	// When running on Cloud Run we need to connect using Unix Sockets.
	// See https://cloud.google.com/sql/docs/mysql/connect-run#go
	if config.OnGCP {
		return unixDsn(config)
	} else {
		return tcpDsn(config)
	}
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

func (db *Orm) RunMigrations(migrations []*Migration) error {
	options := gormigrate.DefaultOptions
	options.UseTransaction = db.config.MigrateUseTransaction
	ms := make([]*gormigrate.Migration, 0, len(migrations))
	for _, m := range migrations {
		ms = append(ms, &gormigrate.Migration{
			ID:       m.ID,
			Migrate:  m.Migrate,
			Rollback: m.Rollback,
		})
	}

	m := gormigrate.New(db.DB, options, ms)

	if err := m.Migrate(); err != nil {
		return err
	}

	return nil
}
