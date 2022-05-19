# orm

For Object Relational Mappings we use [GORM](https://gorm.io/index.html) which is wrapped here and configured to support accessing "Cloud SQL for MySQL" from services running on Cloud Run. Connecting to a locally running MySQL is of course also possible, as well as creating an in-memory SQLite database to use in tests. For performance reasons, the underlying connection pool is preconfigured in accordance to this great read: [Configuring sql.DB for Better Performance](https://www.alexedwards.net/blog/configuring-sqldb), so there should be no need for any custom configuration (all though it is possible).

Opentelemetry instrumentation is also configured via the [otelgorm](https://github.com/uptrace/opentelemetry-go-extra/tree/main/otelgorm) plugin which records database queries and reports metrics for the current span. In order for the Opentelemetry plugin to work, it is vital that "WithContext(ctx)" is used!!! See the example below.

Do also check out the [dentech-floss/pagination](https://github.com/dentech-floss/pagination) lib which goes hand in hand with this lib.

## Install

```
go get github.com/dentech-floss/orm@v0.1.0
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

    "github.com/dentech-floss/orm/pkg/orm"
)

func main() {

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            DbName: "clinic",
            DbUser: "some_user",
            DbPassword: "some_pwd",
            DbHost: "some_host",
            // this is optional, it defaults to false but shall be set to true when on Cloud Run.
            // See https://cloud.google.com/sql/docs/mysql/connect-run#go
            UseUnixSocket: true,
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

You can define a func that provides models for [auto migration](https://gorm.io/docs/migration.html#Auto-Migration) like this:

```go
package example

import (
    "github.com/dentech-floss/orm/pkg/orm"
)

func main() {

    // use this is you want to the run migrations at startup of the service
    migrateModels := func() []interface{} {
        return []interface{}{&model.Clinic{}}
    }

    orm := orm.NewMySqlOrm(
        &orm.OrmConfig{
            DbName: "clinic",
            DbUser: "some_user",
            DbPassword: "some_pwd",
            DbHost: "some_host",
            // provide the func if you to run migrations of models, otherwise just skip this
            MigrateModels: &migrateModels,
        },
    )
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

    migrateModels := func() []interface{} {
        return []interface{}{&model.Clinic{}}
    }

    // create an in-memory database for testing
    orm := orm.NewSQLiteOrm(
        &orm.OrmConfig{
            MigrateModels: &migrateModels, // create the table(s)
        },
    )

    repo := repository.NewSqlRepository(orm)
    patientGatewayServiceV1 := service.NewPatientGatewayServiceV1(repo) // we could inject a mock here otherwise

    patientGatewayServiceV1.FindClinicById(...)
}
```
