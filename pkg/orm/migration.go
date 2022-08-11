package orm

import (
	"github.com/go-gormigrate/gormigrate/v2"
)

// RunMigrations - apply migrations that weren't applied before
func (db *Orm) RunMigrations(
	options *gormigrate.Options,
	migrations []*gormigrate.Migration,
) error {
	if options == nil {
		options = gormigrate.DefaultOptions
	}

	m := gormigrate.New(db.DB, options, migrations)

	if err := m.Migrate(); err != nil {
		return err
	}

	return nil
}

// RunMigrationsInSingleTransaction - apply migrations (inside a single transaction) that weren't applied before
func (db *Orm) RunMigrationsInSingleTransaction(
	migrations []*gormigrate.Migration,
) error {
	options := gormigrate.DefaultOptions
	options.UseTransaction = true
	return db.RunMigrations(options, migrations)
}
