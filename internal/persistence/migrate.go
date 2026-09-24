package persistence

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every pending migration embedded in the binary against
// the database at dsn, using a single shared schema. It is a no-op if the
// schema is already at the latest version.
func Migrate(dsn string) error {
	sqlDB, err := sql.Open("pgx", withoutPoolParams(dsn))
	if err != nil {
		return fmt.Errorf("open postgres connection for migrations: %w", err)
	}
	defer sqlDB.Close()

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	dbDriver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create postgres migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// poolParamPrefix marks the connection-string settings that configure
// pgxpool itself rather than the server connection.
const poolParamPrefix = "pool_"

// withoutPoolParams strips pgxpool's own settings (pool_max_conns and
// friends) from dsn.
//
// Connect uses pgxpool, which understands them; Migrate uses pgx's
// database/sql driver, which does not - it forwards anything it does not
// recognise to the server as a runtime parameter, and Postgres rejects
// the connection with `unrecognized configuration parameter`.
//
// That mattered more than a failed migration: migration failure is
// deliberately non-fatal, so an operator sizing the pool through the DSN
// - the only way to size it today - would get a warning at startup and a
// console running without its activity recorder or token-revocation
// tracking, both of which are skipped when migrations did not apply.
//
// A malformed DSN is returned unchanged rather than reported here, so it
// fails where it already did, with the connection error that names the
// real problem.
func withoutPoolParams(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	stripped := false
	for name := range q {
		if strings.HasPrefix(name, poolParamPrefix) {
			q.Del(name)
			stripped = true
		}
	}
	if !stripped {
		return dsn
	}
	u.RawQuery = q.Encode()
	return u.String()
}
