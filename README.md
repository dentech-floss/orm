# orm

For Object Relational Mappings we use [GORM](https://gorm.io/index.html) which is wrapped here and configured to support accessing "Cloud SQL for MySQL" from services running on Cloud Run. Connecting to a locally running MySQL is of course also possible, as well as creating an in-memory SQLite database to use in tests. 

For performance reasons, the underlying connection pool is preconfigured in accordance to this great read: [Configuring sql.DB for Better Performance](https://www.alexedwards.net/blog/configuring-sqldb), so there should be no need for any custom configuration (all though it is possible).

Opentelemetry instrumentation is also configured via the [otelgorm](https://github.com/uptrace/opentelemetry-go-extra/tree/main/otelgorm) plugin which records database queries and reports metrics for the current span. In order for the Opentelemetry plugin to work, it is vital that "WithContext(ctx)" is used!!! See the example below.

Do also check out the [dentech-floss/pagination](https://github.com/dentech-floss/pagination) lib which goes hand in hand with this lib.

## Install

```
go get github.com/dentech-floss/orm@v0.1.11
```

## Usage

Given this simple model we illustrate the usage of this lib with a quite extensive example, all this layering is obviously not required but this design/abstraction makes the code flexible/mockable and loosly coupled to the actual underlying datasource. So it's easy to swap the datasource, like injecting a SQLite instance while testing for example.

```go
package model

type Clinic struct {
    ID          int32 `gorm:"primaryKey; autoIncrement"`
    Name        *string `gorm:"size:100"`
    Description *string `gorm:"size:255"`
    Created     time.Time
}
```

Create an ORM instance and inject it into an implementation of the repository tier, which then is injected into the service(s):

```go
package example

import (
    "github.com/dentechse/some-service/internal/domain/repository"
    "github.com/dentechse/some-service/pkg/service"

    "github.com/dentech-floss/metadata/pkg/metadata"
    "github.com/dentech-floss/orm/pkg/orm"
)

func main() {

    metadata := metadata.NewMetadata()

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            OnGCP:      metadata.OnGCP,
            DbName:     "clinic",
            DbUser:     "some_user",
            DbPassword: "some_pwd",
            DbHost:     "some_host",
            // DbPort:     3306, // not mandatory, will default to 3306 if not provided
        },
    )

    repo := repository.NewSqlRepository(orm)
    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(repo)
}
```

For the sake of completeness, here is the mentioned repository interface:

```go
package repository

import (
    "context"

    "github.com/dentechse/some-service/internal/domain/model"
    "github.com/dentech-floss/orm/pkg/orm"
)

type Repository interface {
    // returns nil if the clinic was not found
    FindClinicById(ctx context.Context, clinicId int32) (*model.Clinic, error)
}
```

...and its sql implementation based on GORM (notice that "WithContext(ctx)" must be used for the tracing to work):

```go
package repository

import (
    "github.com/dentechse/some-service/internal/domain/model"
    "github.com/dentech-floss/orm/pkg/orm"

    "gorm.io/gorm"
)

type sqlRepository struct {
    orm *orm.Orm
}

func NewSqlRepository(orm *orm.Orm) Repository {
    return &sqlRepository{orm: orm}
}

func (r *sqlRepository) FindClinicById(ctx context.Context, clinicId int32) (*model.Clinic, error) {

	var clinic *model.Clinic = nil
	if err := r.orm.
		WithContext(ctx). // to propagate the active span for tracing!!!
		First(&clinic, clinicId).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return clinic, nil
}
```

### Migration

This lib includes [Gormigrate](https://github.com/go-gormigrate/gormigrate) which is a minimalistic migration helper for GORM that provide support for schema versioning and migration rollback. Gormigrate is in other words more advanced and robust/reliable to use instead of the standard [GORM Migrator Interface](https://gorm.io/docs/migration.html#Migrator-Interface) so this is the recommeded approach, but both options are supported as shown in the below examples.

#### Gormigrate

```go
package example

import (
    "github.com/dentech-floss/metadata/pkg/metadata"
    "github.com/dentech-floss/orm/pkg/migration"
    "github.com/dentech-floss/orm/pkg/orm"

    "gorm.io/gorm"
    "github.com/go-gormigrate/gormigrate/v2"
)

func main() {

    metadata := metadata.NewMetadata()

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            OnGCP:      metadata.OnGCP,
            DbName:     "clinic",
            DbUser:     "some_user",
            DbPassword: "some_pwd",
            DbHost:     "some_host",
        },
    )

    if err := runMigrations(orm); err != nil {
        panic(err)
    }
}

func runMigrations(orm *orm.Orm) error {
    // Or use 'migration.NewMigration(orm, migration.WithUseTransaction)' to just enable the 'UseTransaction' options flag
    return migration.
        NewMigration(orm).
        RunMigrations([]*gormigrate.Migration{
        // create persons table
        {
            ID: "201608301400",
            Migrate: func(tx *gorm.DB) error {
                // it's a good pratice to copy the struct inside the function,
                // so side effects are prevented if the original struct changes during the time
                type Person struct {
                    gorm.Model
                    Name string
                }
                return tx.AutoMigrate(&Person{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropTable("persons")
            },
        },
        // add age column to persons
        {
            ID: "201608301415",
            Migrate: func(tx *gorm.DB) error {
                // when table already exists, it just adds fields as columns
                type Person struct {
                    Age int
                }
                return tx.AutoMigrate(&Person{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropColumn("persons", "age")
            },
        },
        // add pets table
        {
            ID: "201608301430",
            Migrate: func(tx *gorm.DB) error {
                type Pet struct {
                    gorm.Model
                    Name     string
                    PersonID int
                }
                return tx.AutoMigrate(&Pet{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropTable("pets")
            },
        },
    })
}

```

You can rollback last applied migration (could be run more times):
```go
package example

import (
    "github.com/dentech-floss/metadata/pkg/metadata"
    "github.com/dentech-floss/orm/pkg/orm"
    "github.com/dentech-floss/orm/pkg/migration"

    "gorm.io/gorm"
    "github.com/go-gormigrate/gormigrate/v2"
)

func main() {

    metadata := metadata.NewMetadata()

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            OnGCP:      metadata.OnGCP,
            DbName:     "clinic",
            DbUser:     "some_user",
            DbPassword: "some_pwd",
            DbHost:     "some_host",
        },
    )

    if err := runRollback(orm); err != nil {
        panic(err)
    }
}

func runRollback(orm *orm.Orm) error {
    // Or use 'migration.NewMigration(orm, migration.WithUseTransaction)' to just enable the 'UseTransaction' options flag
    return migration.
        NewMigration(orm).
        RollbackLastMigration([]*gormigrate.Migration{
        // create persons table
        {
            ID: "201608301400",
            Migrate: func(tx *gorm.DB) error {
                // it's a good pratice to copy the struct inside the function,
                // so side effects are prevented if the original struct changes during the time
                type Person struct {
                    gorm.Model
                    Name string
                }
                return tx.AutoMigrate(&Person{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropTable("persons")
            },
        },
        // add age column to persons
        {
            ID: "201608301415",
            Migrate: func(tx *gorm.DB) error {
                // when table already exists, it just adds fields as columns
                type Person struct {
                    Age int
                }
                return tx.AutoMigrate(&Person{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropColumn("persons", "age")
            },
        },
        // add pets table
        {
            ID: "201608301430",
            Migrate: func(tx *gorm.DB) error {
                type Pet struct {
                    gorm.Model
                    Name     string
                    PersonID int
                }
                return tx.AutoMigrate(&Pet{})
            },
            Rollback: func(tx *gorm.DB) error {
                return tx.Migrator().DropTable("pets")
            },
        },
    })
}


```

#### GORM Migrator Interface

If you for some reason do not want to use Gormigrate, then you can get hold of the standard [GORM Migrator Interface](https://gorm.io/docs/migration.html#Migrator-Interface) and for example it's [Auto Migration](https://gorm.io/docs/migration.html#Auto-Migration) like this:

```go
package example

import (
    "github.com/dentech-floss/metadata/pkg/metadata"
    "github.com/dentech-floss/orm/pkg/orm"
)

func main() {

    metadata := metadata.NewMetadata()

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            OnGCP:      metadata.OnGCP,
            DbName:     "clinic",
            DbUser:     "some_user",
            DbPassword: "some_pwd",
            DbHost:     "some_host",
        },
    )

    if err := orm.Migrator().AutoMigrate(&model.Clinic{}); err != nil {
        panic(err)
    }
}
```

### Testing

To create and inject an in-memory SQLite database for testing:

```go
package example

import (
    "testing"
    "github.com/dentech-floss/orm/pkg/orm"
)

func Test_FindClinicById(t *testing.T) {

    // in-memory database for testing (and create the table)
    orm := orm.NewSQLiteOrm(&orm.OrmConfig{})

    // ...or use the Gormigrate support as described above...
    if err := orm.GetMigrator().AutoMigrate(&model.Clinic{}); err != nil {
        panic(err)
    }

    repo := repository.NewSqlRepository(orm)
    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(repo) // we could inject a mock here otherwise

    patientGatewayServiceV1.FindClinicById(...)
}
```
