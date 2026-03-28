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
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultMySQLPort    = 3306
	DefaultPostgresPort = 5432
	DirPerms            = 0755
	TimestampFormat     = "2006-01-02_15-04-05"
)

// --- Configuration & Runner ---

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
}

type BackupRunner struct {
	Config    Config
	DBName    string
	BackupDir string
}

func (r *BackupRunner) Run() error {
	if err := os.MkdirAll(r.BackupDir, DirPerms); err != nil {
		return fmt.Errorf("error creating backup directory '%s': %w", r.BackupDir, err)
	}

	fullPath := r.generateFilePath()

	if dumpErr := r.executeDump(fullPath); dumpErr != nil {
		os.Remove(fullPath)
		return dumpErr
	}

	return nil
}

func (r *BackupRunner) generateFilePath() string {
	timestamp := time.Now().Format(TimestampFormat)
	fileName := fmt.Sprintf("%s_%s.sql", r.DBName, timestamp)
	return filepath.Join(r.BackupDir, fileName)
}

// isPostgres returns true when the configured database type is PostgreSQL.
func (r *BackupRunner) isPostgres() bool {
	return strings.ToLower(r.Config.Database.Type) == "postgres"
}

// passwordEnvVar returns the correct env var name for the configured db type.
func (r *BackupRunner) passwordEnvVar() string {
	if r.isPostgres() {
		return "PGPASSWORD"
	}
	return "MYSQL_PWD"
}

// buildArgsPostgres builds psql / pg_dump arguments (no inline password).
func (r *BackupRunner) buildArgsPostgres(isDump bool) []string {
	args := []string{
		fmt.Sprintf("--username=%s", r.Config.Database.User),
		fmt.Sprintf("--host=%s", r.Config.Database.Host),
		fmt.Sprintf("--port=%d", r.Config.Database.Port),
	}
	if isDump && r.DBName != "" {
		args = append(args, r.DBName)
	}
	return args
}

func (r *BackupRunner) buildArgs(tmpFileName string, isDump bool) []string {
	args := []string{
		fmt.Sprintf("--defaults-extra-file=%s", tmpFileName),
		fmt.Sprintf("--user=%s", r.Config.Database.User),
		fmt.Sprintf("--host=%s", r.Config.Database.Host),
		fmt.Sprintf("--port=%d", r.Config.Database.Port),
	}
	if r.Config.SSL.Enabled {
		args = append(args,
			fmt.Sprintf("--ssl-ca=%s", r.Config.SSL.CA),
			fmt.Sprintf("--ssl-cert=%s", r.Config.SSL.Cert),
			fmt.Sprintf("--ssl-key=%s", r.Config.SSL.Key),
		)
		if r.Config.SSL.VerifyServerCert {
			args = append(args, "--ssl-verify-server-cert")
		}
	}
	if isDump && r.DBName != "" {
		args = append(args, r.DBName)
	}
	return args
}

// buildArgsRemote builds mysql/mysqldump args without password (password via MYSQL_PWD env var)
func (r *BackupRunner) buildArgsRemote(isDump bool) []string {
	args := []string{
		fmt.Sprintf("--user=%s", r.Config.Database.User),
		fmt.Sprintf("--host=%s", r.Config.Database.Host),
		fmt.Sprintf("--port=%d", r.Config.Database.Port),
	}
	if r.Config.SSL.Enabled {
		args = append(args,
			fmt.Sprintf("--ssl-ca=%s", r.Config.SSL.CA),
			fmt.Sprintf("--ssl-cert=%s", r.Config.SSL.Cert),
			fmt.Sprintf("--ssl-key=%s", r.Config.SSL.Key),
		)
		if r.Config.SSL.VerifyServerCert {
			args = append(args, "--ssl-verify-server-cert")
		}
	}
	if isDump && r.DBName != "" {
		args = append(args, r.DBName)
	}
	return args
}

// buildRemoteCommand wraps a mysql/mysqldump or psql/pg_dump command for docker or kubernetes execution.
// Password is injected via MYSQL_PWD / PGPASSWORD environment variable (no temp file needed).
func (r *BackupRunner) buildRemoteCommand(executable string, dbArgs []string) (*exec.Cmd, error) {
	remote := r.Config.Remote
	pwdEnv := r.passwordEnvVar() + "=" + r.Config.Database.Password

	switch strings.ToLower(remote.Type) {
	case "docker":
		if remote.Container == "" {
			return nil, fmt.Errorf("remote.container must be set for docker mode")
		}
		args := []string{"exec", "-e", pwdEnv, remote.Container, executable}
		args = append(args, dbArgs...)
		return exec.Command("docker", args...), nil

	case "kubernetes", "k8s":
		if remote.Pod == "" {
			return nil, fmt.Errorf("remote.pod must be set for kubernetes mode")
		}
		args := []string{"exec"}
		if remote.Namespace != "" {
			args = append(args, "-n", remote.Namespace)
		}
		args = append(args, remote.Pod)
		if remote.Container != "" {
			args = append(args, "-c", remote.Container)
		}
		// Use "env" inside the container to set the password var without shell quoting issues
		args = append(args, "--", "env", pwdEnv, executable)
		args = append(args, dbArgs...)
		return exec.Command("kubectl", args...), nil

	default:
		return nil, fmt.Errorf("unknown remote type: %q", remote.Type)
	}
}

func (r *BackupRunner) executeDump(destPath string) error {
	var cmd *exec.Cmd

	remoteType := strings.ToLower(r.Config.Remote.Type)
	isRemote := remoteType == "docker" || remoteType == "kubernetes" || remoteType == "k8s"

	if r.isPostgres() {
		pgArgs := r.buildArgsPostgres(true)
		if isRemote {
			var err error
			cmd, err = r.buildRemoteCommand(r.Config.Remote.PgdumpBin, pgArgs)
			if err != nil {
				return err
			}
		} else {
			// Local mode: password via PGPASSWORD env var
			cmd = exec.Command(r.Config.Remote.PgdumpBin, pgArgs...)
			cmd.Env = append(os.Environ(), "PGPASSWORD="+r.Config.Database.Password)
		}
	} else {
		if isRemote {
			mysqlArgs := r.buildArgsRemote(true)
			var err error
			cmd, err = r.buildRemoteCommand(r.Config.Remote.MysqldumpBin, mysqlArgs)
			if err != nil {
				return err
			}
		} else {
			// Local mode: password via --defaults-extra-file
			tmpFile, err := os.CreateTemp("", "sqldumper-*.cnf")
			if err != nil {
				return fmt.Errorf("error creating temp config: %w", err)
			}
			defer os.Remove(tmpFile.Name())
			fmt.Fprintf(tmpFile, "[client]\npassword=%s\n", r.Config.Database.Password)
			tmpFile.Close()

			args := r.buildArgs(tmpFile.Name(), true)
			cmd = exec.Command(r.Config.Remote.MysqldumpBin, args...)
		}
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("error creating backup file: %w", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		dumpBin := r.Config.Remote.PgdumpBin
		if !r.isPostgres() {
			dumpBin = r.Config.Remote.MysqldumpBin
		}
		return fmt.Errorf("%s failed: %v, stderr: %s", dumpBin, err, stderr.String())
	}
	return nil
}

// FetchDatabases fetches the list of databases via the mysql/psql CLI (local, docker or kubernetes).
func (r *BackupRunner) FetchDatabases() ([]string, error) {
	var cmd *exec.Cmd

	remoteType := strings.ToLower(r.Config.Remote.Type)
	isRemote := remoteType == "docker" || remoteType == "kubernetes" || remoteType == "k8s"

	if r.isPostgres() {
		// List non-template databases excluding the 'postgres' maintenance db
		pgArgs := r.buildArgsPostgres(false)
		pgArgs = append(pgArgs, "-t", "-A", "-c",
			"SELECT datname FROM pg_database WHERE datistemplate = false AND datname <> 'postgres';")
		if isRemote {
			var err error
			cmd, err = r.buildRemoteCommand(r.Config.Remote.PsqlBin, pgArgs)
			if err != nil {
				return nil, err
			}
		} else {
			cmd = exec.Command(r.Config.Remote.PsqlBin, pgArgs...)
			cmd.Env = append(os.Environ(), "PGPASSWORD="+r.Config.Database.Password)
		}
	} else {
		if isRemote {
			mysqlArgs := r.buildArgsRemote(false)
			mysqlArgs = append(mysqlArgs, "-s", "-N", "-e", "SHOW DATABASES;")
			var err error
			cmd, err = r.buildRemoteCommand(r.Config.Remote.MysqlBin, mysqlArgs)
			if err != nil {
				return nil, err
			}
		} else {
			// Local mode: password via --defaults-extra-file
			tmpFile, err := os.CreateTemp("", "sqlclient-*.cnf")
			if err != nil {
				return nil, fmt.Errorf("error creating temp config: %w", err)
			}
			defer os.Remove(tmpFile.Name())
			fmt.Fprintf(tmpFile, "[client]\npassword=%s\n", r.Config.Database.Password)
			tmpFile.Close()

			args := r.buildArgs(tmpFile.Name(), false)
			// -s (silent), -N (no column names), -e (execute)
			args = append(args, "-s", "-N", "-e", "SHOW DATABASES;")
			cmd = exec.Command(r.Config.Remote.MysqlBin, args...)
		}
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch databases: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var dbs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Filter out MySQL system databases
		if line == "information_schema" || line == "performance_schema" || line == "sys" {
			continue
		}
		dbs = append(dbs, line)
	}
	return dbs, nil
}

// --- Main ---

func main() {
	dbName := flag.String("db", "", "Name of the database (if missing, interactive multiselect opens)")
	backupDir := flag.String("dir", "./backup", "Directory where the backup should be saved")
	configFile := flag.String("config", "./easy_sql_config.toml", "Path to the TOML configuration file")
	dbType := flag.String("type", "", "Database type: \"mysql\" or \"postgres\" (overrides config)")
	flag.Parse()

	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	// CLI flag overrides config
	if *dbType != "" {
		config.Database.Type = *dbType
	}

	runner := &BackupRunner{
		Config:    config,
		DBName:    *dbName,
		BackupDir: *backupDir,
	}

	// Case 1: CLI mode (scripting / cronjob) — single database via -db flag
	if *dbName != "" {
		fmt.Printf("⏳ Creating backup of database '%s'...\n", runner.DBName)
		if err := runner.Run(); err != nil {
			log.Fatalf("❌ Backup failed: %v\n", err)
		}
		fmt.Println("✅ Backup successfully created.")
		os.Exit(0)
	}

	// Case 2: Interactive multiselect mode
	fmt.Println("🔌☁️ Connecting to DBMS and loading databases...")
	dbs, err := runner.FetchDatabases()
	if err != nil {
		log.Fatalf("❌ Error fetching databases: %v", err)
	}
	if len(dbs) == 0 {
		fmt.Println("❌ No databases found (or insufficient privileges).")
		os.Exit(1)
	}

	// Build huh options from the database list
	opts := make([]huh.Option[string], len(dbs))
	for i, db := range dbs {
		opts[i] = huh.NewOption(db, db)
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select databases to back up").
				Description("ctrl+c: quit").
				Options(opts...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		// User cancelled (ctrl+c / esc)
		fmt.Println("Cancelled.")
		os.Exit(0)
	}

	if len(selected) == 0 {
		fmt.Println("No databases selected. Nothing to do.")
		os.Exit(0)
	}

	// Run backups for all selected databases
	fmt.Printf("\nStarting backup for %d database(s)...\n\n", len(selected))
	var failed []string
	for _, db := range selected {
		r := &BackupRunner{
			Config:    config,
			DBName:    db,
			BackupDir: *backupDir,
		}
		fmt.Printf("⏳  %-30s", db)
		if err := r.Run(); err != nil {
			fmt.Printf("❌ failed: %v\n", err)
			failed = append(failed, db)
		} else {
			fmt.Println("✅ done")
		}
	}

	fmt.Println()
	if len(failed) > 0 {
		fmt.Printf("❌ %d backup(s) failed: %s\n", len(failed), strings.Join(failed, ", "))
		os.Exit(1)
	}
	fmt.Printf("✅ All %d backup(s) completed successfully.\n", len(selected))
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

	return config, nil
}
