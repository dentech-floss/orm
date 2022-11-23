package migration

import (
	"github.com/dentech-floss/orm/pkg/orm"
	"github.com/go-gormigrate/gormigrate/v2"
)

// Migration - migration object structure
type Migration struct {
	db      *orm.Orm
	options *gormigrate.Options
}

// Option - type for function for options change
type Option func(*gormigrate.Options) *gormigrate.Options

// NewMigration - create migration instance
func NewMigration(db *orm.Orm, options ...Option) *Migration {
	goptions := &gormigrate.Options{}
	*goptions = *gormigrate.DefaultOptions
	for _, f := range options {
		goptions = f(goptions)
	}

	return &Migration{
		db:      db,
		options: goptions,
	}
}

// RunMigrations - apply migrations that weren't applied before
func (m Migration) RunMigrations(
	migrations []*gormigrate.Migration,
) error {
	gm := gormigrate.New(m.db.DB, m.options, migrations)

	if err := gm.Migrate(); err != nil {
		return err
	}

	return nil
}

// RollbackLastMigration - rollback last applied migration
func (m Migration) RollbackLastMigration(
	migrations []*gormigrate.Migration,
) error {
	gm := gormigrate.New(m.db.DB, m.options, migrations)

	if err := gm.RollbackLast(); err != nil {
		return err
	}

	return nil
}

// WithUseTransaction - add UseTransaction = true to options
func WithUseTransaction(o *gormigrate.Options) *gormigrate.Options {
	o.UseTransaction = true
	return o
}
