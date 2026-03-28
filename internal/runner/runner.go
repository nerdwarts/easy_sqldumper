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

package runner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"easy_sqldumper/internal/config"
)

// BackupRunner orchestrates a single database backup.
type BackupRunner struct {
	Config    config.Config
	DBName    string
	BackupDir string
}

// Run creates the backup directory if needed and dumps the database to a
// timestamped SQL file, cleaning up on failure.
func (r *BackupRunner) Run() error {
	if err := os.MkdirAll(r.BackupDir, config.DirPerms); err != nil {
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
	timestamp := time.Now().Format(config.TimestampFormat)
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

// buildArgs builds mysql / mysqldump arguments for local mode (password via --defaults-extra-file).
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

// buildArgsRemote builds mysql/mysqldump args without password (password via MYSQL_PWD env var).
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
		// List non-template databases, excluding the 'postgres' maintenance db
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
