package orm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
)

func testConfig() *OrmConfig {
	config := &OrmConfig{
		DbName:     "clinic",
		DbUser:     "user",
		DbPassword: "secret",
		DbHost:     "db.example.com",
	}
	config.setDefaults(defaultMySQLLogger)
	return config
}

// writeCA writes a file with the given number of newly generated CA certificates.
func writeCA(t *testing.T, count int) string {
	t.Helper()

	var content []byte
	for i := range count {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{
			SerialNumber:          big.NewInt(int64(i + 1)),
			Subject:               pkix.Name{CommonName: "test CA"},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			IsCA:                  true,
			BasicConstraintsValid: true,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		content = append(content, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}

	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDsnOnGCPIsUnchanged(t *testing.T) {
	t.Setenv("DB_SOCKET_DIR", "cloudsql")
	t.Setenv("DB_TLS_CA_FILE", "/does/not/exist.pem")

	config := testConfig()
	config.OnGCP = true

	want := "user:secret@unix(/cloudsql/db.example.com)/clinic?charset=utf8mb4&parseTime=true"
	if got := dsn(config); got != want {
		t.Errorf("dsn() = %q, want %q", got, want)
	}
}

func TestDsnTCPWithoutTLS(t *testing.T) {
	t.Setenv("DB_TLS_CA_FILE", "")

	want := "user:secret@tcp(db.example.com:3306)/clinic?charset=utf8mb4&parseTime=true"
	if got := dsn(testConfig()); got != want {
		t.Errorf("dsn() = %q, want %q", got, want)
	}
}

func TestDsnTCPWithTLS(t *testing.T) {
	tests := map[string]struct {
		certs   int
		fromEnv bool
	}{
		"one CA":       {certs: 1},
		"several CAs":  {certs: 2},
		"from the env": {certs: 1, fromEnv: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeCA(t, test.certs)
			t.Setenv("DB_TLS_CA_FILE", "")

			config := testConfig()
			if test.fromEnv {
				t.Setenv("DB_TLS_CA_FILE", path)
				config.setDefaults(defaultMySQLLogger)
			} else {
				config.TLSCAFile = path
			}

			// ParseDSN fails if the named TLS config isn't registered.
			parsed, err := gomysql.ParseDSN(dsn(config))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(parsed.TLSConfig, "orm-") {
				t.Errorf("TLSConfig = %q, want a registered config", parsed.TLSConfig)
			}
			if parsed.TLS == nil {
				t.Fatal("TLS is nil")
			}
			if parsed.TLS.InsecureSkipVerify {
				t.Error("the server's certificate isn't verified")
			}
			if parsed.TLS.ServerName != "db.example.com" {
				t.Errorf("ServerName = %q, want the database host", parsed.TLS.ServerName)
			}
			if got := len(parsed.TLS.RootCAs.Subjects()); got != test.certs { //nolint:staticcheck // only the count matters
				t.Errorf("trusts %d CAs, want %d", got, test.certs)
			}
		})
	}
}

func TestDsnTLSConfigsDoNotClash(t *testing.T) {
	first, second := testConfig(), testConfig()
	first.TLSCAFile, second.TLSCAFile = writeCA(t, 1), writeCA(t, 1)

	if dsn(first) == dsn(second) {
		t.Error("two configs share a TLS config name")
	}
}

func TestDsnPanicsOnUnusableCAFile(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"missing":         filepath.Join(t.TempDir(), "missing.pem"),
		"no certificates": empty,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic: this would connect without the requested TLS")
				}
			}()

			config := testConfig()
			config.TLSCAFile = path
			dsn(config)
		})
	}
}
