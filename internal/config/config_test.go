// Copyright 2026 Nerdwarts
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// writeTOML writes content to a temp file and returns its path.
func writeTOML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// writeTempFile creates a temporary file with optional content.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ssl-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

// ── LoadConfig ────────────────────────────────────────────────────────────────

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	path := writeTOML(t, "this is not valid TOML ][")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

func TestLoadConfig_DefaultType_IsMySQL(t *testing.T) {
	path := writeTOML(t, `
[database]
user = "root"
password = "pass"
host = "localhost"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Type != "mysql" {
		t.Errorf("expected default type 'mysql', got %q", cfg.Database.Type)
	}
}

func TestLoadConfig_DefaultPort_MySQL(t *testing.T) {
	path := writeTOML(t, `
[database]
user = "root"
password = "pass"
host = "localhost"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Port != DefaultMySQLPort {
		t.Errorf("expected port %d, got %d", DefaultMySQLPort, cfg.Database.Port)
	}
}

func TestLoadConfig_DefaultPort_Postgres(t *testing.T) {
	path := writeTOML(t, `
[database]
type = "postgres"
user = "postgres"
password = "pass"
host = "localhost"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Port != DefaultPostgresPort {
		t.Errorf("expected port %d, got %d", DefaultPostgresPort, cfg.Database.Port)
	}
}

func TestLoadConfig_ExplicitPort_IsRespected(t *testing.T) {
	path := writeTOML(t, `
[database]
user = "root"
password = "pass"
host = "localhost"
port = 3307
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Port != 3307 {
		t.Errorf("expected port 3307, got %d", cfg.Database.Port)
	}
}

func TestLoadConfig_DefaultBinaries(t *testing.T) {
	path := writeTOML(t, `
[database]
user = "root"
password = "pass"
host = "localhost"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Remote.MysqlBin != "mysql" {
		t.Errorf("expected mysql_bin 'mysql', got %q", cfg.Remote.MysqlBin)
	}
	if cfg.Remote.MysqldumpBin != "mysqldump" {
		t.Errorf("expected mysqldump_bin 'mysqldump', got %q", cfg.Remote.MysqldumpBin)
	}
	if cfg.Remote.PsqlBin != "psql" {
		t.Errorf("expected psql_bin 'psql', got %q", cfg.Remote.PsqlBin)
	}
	if cfg.Remote.PgdumpBin != "pg_dump" {
		t.Errorf("expected pgdump_bin 'pg_dump', got %q", cfg.Remote.PgdumpBin)
	}
}

func TestLoadConfig_CustomBinaries(t *testing.T) {
	path := writeTOML(t, `
[database]
user = "root"
password = "pass"
host = "localhost"

[remote]
mysql_bin     = "mariadb"
mysqldump_bin = "mariadb-dump"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Remote.MysqlBin != "mariadb" {
		t.Errorf("expected mysql_bin 'mariadb', got %q", cfg.Remote.MysqlBin)
	}
	if cfg.Remote.MysqldumpBin != "mariadb-dump" {
		t.Errorf("expected mysqldump_bin 'mariadb-dump', got %q", cfg.Remote.MysqldumpBin)
	}
}

func TestLoadConfig_PasswordResolvedFromEnv(t *testing.T) {
	t.Setenv("TEST_DB_PASS", "env-resolved-pass")
	path := writeTOML(t, `
[database]
user = "root"
password = "env:TEST_DB_PASS"
host = "localhost"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.Password != "env-resolved-pass" {
		t.Errorf("expected resolved password, got %q", cfg.Database.Password)
	}
}

// ── validateSSL ───────────────────────────────────────────────────────────────

func TestValidateSSL_Disabled_NoOrphanedFields(t *testing.T) {
	cfg := Config{}
	cfg.SSL.Enabled = false
	if err := cfg.validateSSL(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateSSL_Disabled_OrphanedFields_NoError(t *testing.T) {
	// Orphaned fields produce a warning on stderr but NOT an error.
	cfg := Config{}
	cfg.SSL.Enabled = false
	cfg.SSL.CA = "/some/ca.pem"
	cfg.SSL.Cert = "/some/cert.pem"
	if err := cfg.validateSSL(); err != nil {
		t.Errorf("expected no error for orphaned fields when SSL disabled, got: %v", err)
	}
}

func TestValidateSSL_PostgresWithSSL_Error(t *testing.T) {
	cfg := Config{}
	cfg.Database.Type = "postgres"
	cfg.SSL.Enabled = true
	if err := cfg.validateSSL(); err == nil {
		t.Error("expected error when SSL enabled for postgres, got nil")
	}
}

func TestValidateSSL_Enabled_MissingCA(t *testing.T) {
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	// CA is empty
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected error for missing CA, got nil")
	}
	if !strings.Contains(err.Error(), "ssl.ca is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSSL_Enabled_CANotFound(t *testing.T) {
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.CA = "/nonexistent/ca.pem"
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected error for non-existent CA file, got nil")
	}
	if !strings.Contains(err.Error(), "ssl.ca") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSSL_Enabled_ValidCAOnly(t *testing.T) {
	caFile := writeTempFile(t, "fake-ca")
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.CA = caFile
	if err := cfg.validateSSL(); err != nil {
		t.Errorf("expected no error with valid CA, got: %v", err)
	}
}

func TestValidateSSL_Enabled_CertWithoutKey(t *testing.T) {
	caFile := writeTempFile(t, "fake-ca")
	certFile := writeTempFile(t, "fake-cert")
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.CA = caFile
	cfg.SSL.Cert = certFile
	// Key is intentionally empty
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected error for cert without key, got nil")
	}
	if !strings.Contains(err.Error(), "ssl.key is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSSL_Enabled_KeyWithoutCert(t *testing.T) {
	caFile := writeTempFile(t, "fake-ca")
	keyFile := writeTempFile(t, "fake-key")
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.CA = caFile
	cfg.SSL.Key = keyFile
	// Cert is intentionally empty
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected error for key without cert, got nil")
	}
	if !strings.Contains(err.Error(), "ssl.cert is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateSSL_Enabled_ValidCertAndKey(t *testing.T) {
	caFile := writeTempFile(t, "fake-ca")
	certFile := writeTempFile(t, "fake-cert")
	keyFile := writeTempFile(t, "fake-key")
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.CA = caFile
	cfg.SSL.Cert = certFile
	cfg.SSL.Key = keyFile
	if err := cfg.validateSSL(); err != nil {
		t.Errorf("expected no error with valid cert+key, got: %v", err)
	}
}

func TestValidateSSL_Enabled_VerifyWithoutCA(t *testing.T) {
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	cfg.SSL.VerifyServerCert = true
	// CA is empty — should produce two errors (missing CA + verify needs CA)
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected error for verify_server_cert without CA, got nil")
	}
	if !strings.Contains(err.Error(), "ssl.verify_server_cert") {
		t.Errorf("expected verify_server_cert error, got: %v", err)
	}
}

func TestValidateSSL_MultipleErrors_Joined(t *testing.T) {
	cfg := Config{}
	cfg.Database.Type = "mysql"
	cfg.SSL.Enabled = true
	// CA missing, cert without key
	cfg.SSL.Cert = "/some/cert.pem"
	err := cfg.validateSSL()
	if err == nil {
		t.Fatal("expected multiple errors, got nil")
	}
	// Should contain at least two distinct error messages joined by "; "
	if !strings.Contains(err.Error(), ";") {
		t.Errorf("expected multiple errors joined by '; ', got: %v", err)
	}
}

// ── fileExists ────────────────────────────────────────────────────────────────

func TestFileExists_RegularFile(t *testing.T) {
	path := writeTempFile(t, "content")
	if err := fileExists(path); err != nil {
		t.Errorf("expected no error for existing file, got: %v", err)
	}
}

func TestFileExists_MissingFile(t *testing.T) {
	err := fileExists(filepath.Join(t.TempDir(), "nonexistent.pem"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFileExists_Directory(t *testing.T) {
	err := fileExists(t.TempDir())
	if err == nil {
		t.Fatal("expected error for directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}
