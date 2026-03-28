// Copyright 2026 Nerdwarts
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package runner

import (
	"regexp"
	"strings"
	"testing"

	"easy_sqldumper/internal/config"
)

func mysqlRunner(dbName string) *BackupRunner {
	cfg := config.Config{}
	cfg.Database.Type = "mysql"
	cfg.Database.User = "root"
	cfg.Database.Password = "secret"
	cfg.Database.Host = "localhost"
	cfg.Database.Port = 3306
	cfg.Remote.MysqlBin = "mysql"
	cfg.Remote.MysqldumpBin = "mysqldump"
	cfg.Remote.PsqlBin = "psql"
	cfg.Remote.PgdumpBin = "pg_dump"
	return &BackupRunner{Config: cfg, DBName: dbName, BackupDir: "/tmp/backups"}
}

func postgresRunner(dbName string) *BackupRunner {
	r := mysqlRunner(dbName)
	r.Config.Database.Type = "postgres"
	r.Config.Database.Port = 5432
	return r
}

// isPostgres / passwordEnvVar

func TestIsPostgres_MySQL(t *testing.T) {
	if mysqlRunner("db").isPostgres() {
		t.Error("expected false for mysql type")
	}
}
func TestIsPostgres_Postgres(t *testing.T) {
	if !postgresRunner("db").isPostgres() {
		t.Error("expected true for postgres type")
	}
}
func TestIsPostgres_CaseInsensitive(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Database.Type = "POSTGRES"
	if !r.isPostgres() {
		t.Error("expected true for POSTGRES (uppercase)")
	}
}
func TestPasswordEnvVar_MySQL(t *testing.T) {
	if got := mysqlRunner("").passwordEnvVar(); got != "MYSQL_PWD" {
		t.Errorf("expected 'MYSQL_PWD', got %q", got)
	}
}
func TestPasswordEnvVar_Postgres(t *testing.T) {
	if got := postgresRunner("").passwordEnvVar(); got != "PGPASSWORD" {
		t.Errorf("expected 'PGPASSWORD', got %q", got)
	}
}

// buildArgsPostgres

func TestBuildArgsPostgres_NoDump(t *testing.T) {
	args := postgresRunner("mydb").buildArgsPostgres(false)
	assertContains(t, args, "--username=root")
	assertContains(t, args, "--host=localhost")
	assertContains(t, args, "--port=5432")
	assertNotContains(t, args, "mydb")
}
func TestBuildArgsPostgres_WithDump(t *testing.T) {
	args := postgresRunner("mydb").buildArgsPostgres(true)
	assertContains(t, args, "mydb")
}

// buildArgs (MySQL local)

func TestBuildArgs_Basic(t *testing.T) {
	r := mysqlRunner("mydb")
	args := r.buildArgs("/tmp/cred.cnf", true)
	assertContains(t, args, "--defaults-extra-file=/tmp/cred.cnf")
	assertContains(t, args, "--user=root")
	assertContains(t, args, "--host=localhost")
	assertContains(t, args, "--port=3306")
	assertContains(t, args, "mydb")
}
func TestBuildArgs_NoDump(t *testing.T) {
	args := mysqlRunner("mydb").buildArgs("/tmp/cred.cnf", false)
	assertNotContains(t, args, "mydb")
}
func TestBuildArgs_WithSSL(t *testing.T) {
	r := mysqlRunner("mydb")
	r.Config.SSL.Enabled = true
	r.Config.SSL.CA = "/etc/ssl/ca.pem"
	r.Config.SSL.Cert = "/etc/ssl/cert.pem"
	r.Config.SSL.Key = "/etc/ssl/key.pem"
	args := r.buildArgs("/tmp/cred.cnf", false)
	assertContains(t, args, "--ssl-ca=/etc/ssl/ca.pem")
	assertContains(t, args, "--ssl-cert=/etc/ssl/cert.pem")
	assertContains(t, args, "--ssl-key=/etc/ssl/key.pem")
	assertNotContains(t, args, "--ssl-verify-server-cert")
}
func TestBuildArgs_WithSSLVerify(t *testing.T) {
	r := mysqlRunner("")
	r.Config.SSL.Enabled = true
	r.Config.SSL.CA = "/ca.pem"
	r.Config.SSL.Cert = "/cert.pem"
	r.Config.SSL.Key = "/key.pem"
	r.Config.SSL.VerifyServerCert = true
	args := r.buildArgs("/tmp/cred.cnf", false)
	assertContains(t, args, "--ssl-verify-server-cert")
}

// buildArgsRemote

func TestBuildArgsRemote_Basic(t *testing.T) {
	args := mysqlRunner("").buildArgsRemote(false)
	assertContains(t, args, "--user=root")
	assertContains(t, args, "--host=localhost")
	assertContains(t, args, "--port=3306")
	for _, a := range args {
		if strings.HasPrefix(a, "--defaults-extra-file") {
			t.Errorf("remote args must not include --defaults-extra-file")
		}
	}
}
func TestBuildArgsRemote_WithDB(t *testing.T) {
	args := mysqlRunner("mydb").buildArgsRemote(true)
	assertContains(t, args, "mydb")
}

// buildRemoteCommand Docker

func TestBuildRemoteCommand_Docker_Basic(t *testing.T) {
	r := mysqlRunner("mydb")
	r.Config.Remote.Type = "docker"
	r.Config.Remote.Container = "my-container"
	cmd, err := r.buildRemoteCommand("mysqldump", []string{"--user=root", "mydb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 0, "docker")
	assertArg(t, cmd.Args, 1, "exec")
	assertArg(t, cmd.Args, 2, "-e")
	assertArg(t, cmd.Args, 3, "MYSQL_PWD=secret")
	assertArg(t, cmd.Args, 4, "my-container")
	assertArg(t, cmd.Args, 5, "mysqldump")
}
func TestBuildRemoteCommand_Docker_PostgresEnvVar(t *testing.T) {
	r := postgresRunner("mydb")
	r.Config.Remote.Type = "docker"
	r.Config.Remote.Container = "pg"
	cmd, err := r.buildRemoteCommand("pg_dump", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 3, "PGPASSWORD=secret")
}
func TestBuildRemoteCommand_Docker_MissingContainer(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "docker"
	_, err := r.buildRemoteCommand("mysqldump", []string{})
	if err == nil || !strings.Contains(err.Error(), "container") {
		t.Errorf("expected container error, got: %v", err)
	}
}

// buildRemoteCommand Kubernetes

func TestBuildRemoteCommand_K8s_Basic(t *testing.T) {
	r := mysqlRunner("mydb")
	r.Config.Remote.Type = "kubernetes"
	r.Config.Remote.Pod = "my-pod"
	cmd, err := r.buildRemoteCommand("mysqldump", []string{"--user=root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 0, "kubectl")
	assertArg(t, cmd.Args, 1, "exec")
	assertArg(t, cmd.Args, 2, "my-pod")
	assertArg(t, cmd.Args, 3, "--")
	assertArg(t, cmd.Args, 4, "env")
	assertArg(t, cmd.Args, 5, "MYSQL_PWD=secret")
	assertArg(t, cmd.Args, 6, "mysqldump")
}
func TestBuildRemoteCommand_K8s_WithNamespace(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "kubernetes"
	r.Config.Remote.Pod = "my-pod"
	r.Config.Remote.Namespace = "production"
	cmd, err := r.buildRemoteCommand("mysqldump", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 2, "-n")
	assertArg(t, cmd.Args, 3, "production")
	assertArg(t, cmd.Args, 4, "my-pod")
}
func TestBuildRemoteCommand_K8s_WithContainer(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "k8s"
	r.Config.Remote.Pod = "my-pod"
	r.Config.Remote.Container = "db"
	cmd, err := r.buildRemoteCommand("mysqldump", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 2, "my-pod")
	assertArg(t, cmd.Args, 3, "-c")
	assertArg(t, cmd.Args, 4, "db")
}
func TestBuildRemoteCommand_K8s_WithNamespaceAndContainer(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "kubernetes"
	r.Config.Remote.Pod = "pod"
	r.Config.Remote.Namespace = "staging"
	r.Config.Remote.Container = "db"
	cmd, err := r.buildRemoteCommand("mysqldump", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArg(t, cmd.Args, 2, "-n")
	assertArg(t, cmd.Args, 3, "staging")
	assertArg(t, cmd.Args, 4, "pod")
	assertArg(t, cmd.Args, 5, "-c")
	assertArg(t, cmd.Args, 6, "db")
}
func TestBuildRemoteCommand_K8s_MissingPod(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "kubernetes"
	_, err := r.buildRemoteCommand("mysqldump", []string{})
	if err == nil || !strings.Contains(err.Error(), "pod") {
		t.Errorf("expected pod error, got: %v", err)
	}
}
func TestBuildRemoteCommand_UnknownType(t *testing.T) {
	r := mysqlRunner("")
	r.Config.Remote.Type = "sftp"
	_, err := r.buildRemoteCommand("mysqldump", []string{})
	if err == nil || !strings.Contains(err.Error(), "sftp") {
		t.Errorf("expected unknown-type error, got: %v", err)
	}
}

// generateFilePath

func TestGenerateFilePath_Format(t *testing.T) {
	r := mysqlRunner("mydb")
	r.BackupDir = "/backups"
	path := r.generateFilePath()
	if !strings.HasPrefix(path, "/backups/mydb_") {
		t.Errorf("unexpected prefix: %s", path)
	}
	if !strings.HasSuffix(path, ".sql") {
		t.Errorf("expected .sql suffix: %s", path)
	}
	re := regexp.MustCompile(`mydb_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.sql$`)
	if !re.MatchString(path) {
		t.Errorf("path does not match timestamp pattern: %s", path)
	}
}
func TestGenerateFilePath_ContainsDBName(t *testing.T) {
	r := mysqlRunner("production_db")
	r.BackupDir = "/var/backups"
	if !strings.Contains(r.generateFilePath(), "production_db") {
		t.Error("expected db name in generated path")
	}
}

// assertion helpers

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("expected arg %q in %v", want, args)
}
func assertNotContains(t *testing.T, args []string, unwanted string) {
	t.Helper()
	for _, a := range args {
		if a == unwanted {
			t.Errorf("unexpected arg %q in %v", unwanted, args)
		}
	}
}
func assertArg(t *testing.T, args []string, i int, want string) {
	t.Helper()
	if i >= len(args) {
		t.Errorf("args[%d] out of range (len=%d), want %q", i, len(args), want)
		return
	}
	got := args[i]
	if got != want && !strings.HasSuffix(got, "/"+want) {
		t.Errorf("args[%d]: expected %q, got %q", i, want, got)
	}
}
