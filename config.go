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

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultMySQLPort    = 3306
	DefaultPostgresPort = 5432
	DirPerms            = 0755
	TimestampFormat     = "2006-01-02_15-04-05"
)

type Config struct {
	Database struct {
		Type     string `toml:"type"` // "mysql" (default) or "postgres"
		User     string `toml:"user"`
		Password string `toml:"password"`
		Host     string `toml:"host"`
		Port     int    `toml:"port"`
	} `toml:"database"`
	SSL struct {
		Enabled          bool   `toml:"enabled"`
		CA               string `toml:"ca"`
		Cert             string `toml:"cert"`
		Key              string `toml:"key"`
		VerifyServerCert bool   `toml:"verify_server_cert"`
	} `toml:"ssl"`
	Remote struct {
		Type         string `toml:"type"`          // "local" (default), "docker", "kubernetes"
		Container    string `toml:"container"`     // Docker: container name; K8s: container name (optional)
		Namespace    string `toml:"namespace"`     // K8s: namespace (optional, uses current context)
		Pod          string `toml:"pod"`           // K8s: pod name
		MysqlBin     string `toml:"mysql_bin"`     // default: "mysql"
		MysqldumpBin string `toml:"mysqldump_bin"` // default: "mysqldump"
		PsqlBin      string `toml:"psql_bin"`      // default: "psql"
		PgdumpBin    string `toml:"pgdump_bin"`    // default: "pg_dump"
	} `toml:"remote"`
	Secrets SecretsConfig `toml:"secrets"`
}

func loadConfig(path string) (Config, error) {
	var config Config
	f, err := os.Open(path)
	if err != nil {
		return config, fmt.Errorf("error opening config: %w", err)
	}
	defer f.Close()

	if err := toml.NewDecoder(f).Decode(&config); err != nil {
		return config, fmt.Errorf("error parsing config: %w", err)
	}

	// Defaults for database type
	if config.Database.Type == "" {
		config.Database.Type = "mysql"
	}
	if config.Database.Port == 0 {
		if strings.ToLower(config.Database.Type) == "postgres" {
			config.Database.Port = DefaultPostgresPort
		} else {
			config.Database.Port = DefaultMySQLPort
		}
	}

	// Defaults for MySQL binaries
	if config.Remote.MysqlBin == "" {
		config.Remote.MysqlBin = "mysql"
	}
	if config.Remote.MysqldumpBin == "" {
		config.Remote.MysqldumpBin = "mysqldump"
	}

	// Defaults for PostgreSQL binaries
	if config.Remote.PsqlBin == "" {
		config.Remote.PsqlBin = "psql"
	}
	if config.Remote.PgdumpBin == "" {
		config.Remote.PgdumpBin = "pg_dump"
	}

	if err := config.validateSSL(); err != nil {
		return config, fmt.Errorf("SSL configuration error: %w", err)
	}

	// Resolve the database password from the configured secret backend
	resolved, err := resolveSecret(config.Database.Password, config.Secrets)
	if err != nil {
		return config, fmt.Errorf("resolving database password: %w", err)
	}
	config.Database.Password = resolved

	return config, nil
}

// validateSSL checks that the SSL configuration is complete and consistent.
func (c Config) validateSSL() error {
	ssl := c.SSL

	// SSL options are MySQL/MariaDB-only in this tool
	if ssl.Enabled && strings.ToLower(c.Database.Type) == "postgres" {
		return errors.New("[ssl] enabled = true is not supported for PostgreSQL in this tool; " +
			"configure TLS on the server side or use a SSL-aware connection string instead")
	}

	if !ssl.Enabled {
		// Warn about orphaned SSL fields when SSL is disabled
		orphaned := []string{}
		if ssl.CA != "" {
			orphaned = append(orphaned, "ca")
		}
		if ssl.Cert != "" {
			orphaned = append(orphaned, "cert")
		}
		if ssl.Key != "" {
			orphaned = append(orphaned, "key")
		}
		if len(orphaned) > 0 {
			// Not a hard error, but surface it clearly
			fmt.Fprintf(os.Stderr, "⚠️  Warning: SSL is disabled but the following fields are set and will be ignored: %s\n",
				strings.Join(orphaned, ", "))
		}
		return nil
	}

	// SSL is enabled — validate required fields and file existence
	var errs []string

	if ssl.CA == "" {
		errs = append(errs, "ssl.ca is required when SSL is enabled")
	} else if err := fileExists(ssl.CA); err != nil {
		errs = append(errs, fmt.Sprintf("ssl.ca: %v", err))
	}

	// cert and key must be provided together
	switch {
	case ssl.Cert != "" && ssl.Key == "":
		errs = append(errs, "ssl.key is required when ssl.cert is set")
	case ssl.Cert == "" && ssl.Key != "":
		errs = append(errs, "ssl.cert is required when ssl.key is set")
	case ssl.Cert != "" && ssl.Key != "":
		if err := fileExists(ssl.Cert); err != nil {
			errs = append(errs, fmt.Sprintf("ssl.cert: %v", err))
		}
		if err := fileExists(ssl.Key); err != nil {
			errs = append(errs, fmt.Sprintf("ssl.key: %v", err))
		}
	}

	if ssl.VerifyServerCert && ssl.CA == "" {
		errs = append(errs, "ssl.verify_server_cert = true requires ssl.ca to be set")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// fileExists returns an error if the path does not exist or is not a regular file.
func fileExists(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("file not found: %q", path)
	}
	if err != nil {
		return fmt.Errorf("cannot access %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, not a file", path)
	}
	return nil
}
