//go:build integration

// Copyright 2026 Nerdwarts
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Integration tests require a running database.
// Run them with:
//
//	# MySQL / MariaDB
//	TEST_MYSQL_HOST=127.0.0.1 TEST_MYSQL_USER=root TEST_MYSQL_PASSWORD=secret \
//	  TEST_MYSQL_DB=mydb go test -tags integration -v ./...
//
//	# PostgreSQL
//	TEST_PG_HOST=127.0.0.1 TEST_PG_USER=postgres TEST_PG_PASSWORD=secret \
//	  TEST_PG_DB=mydb go test -tags integration -v ./...

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy_sqldumper/internal/config"
	"easy_sqldumper/internal/runner"
)

// ── MySQL / MariaDB ───────────────────────────────────────────────────────────

func TestIntegration_MySQL_FetchDatabases(t *testing.T) {
	cfg := integrationMySQLConfig(t)
	r := &runner.BackupRunner{Config: cfg}

	dbs, err := r.FetchDatabases()
	if err != nil {
		t.Fatalf("FetchDatabases failed: %v", err)
	}
	if len(dbs) == 0 {
		t.Fatal("expected at least one database, got none")
	}
	// System databases should be filtered out
	for _, db := range dbs {
		if db == "information_schema" || db == "performance_schema" || db == "sys" {
			t.Errorf("system database %q should be filtered out", db)
		}
	}
	t.Logf("Found databases: %v", dbs)
}

func TestIntegration_MySQL_Backup(t *testing.T) {
	cfg := integrationMySQLConfig(t)
	dbName := requireEnv(t, "TEST_MYSQL_DB")
	dir := t.TempDir()

	r := &runner.BackupRunner{
		Config:    cfg,
		DBName:    dbName,
		BackupDir: dir,
	}

	if err := r.Run(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// Verify backup file was created and is non-empty
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no backup file was created")
	}
	backupFile := filepath.Join(dir, entries[0].Name())
	info, _ := os.Stat(backupFile)
	if info.Size() == 0 {
		t.Errorf("backup file is empty: %s", backupFile)
	}
	if !strings.HasSuffix(backupFile, ".sql") {
		t.Errorf("expected .sql file, got: %s", backupFile)
	}
	t.Logf("Backup created: %s (%d bytes)", backupFile, info.Size())
}

func TestIntegration_MySQL_BackupCleanupOnFailure(t *testing.T) {
	cfg := integrationMySQLConfig(t)
	dir := t.TempDir()

	r := &runner.BackupRunner{
		Config:    cfg,
		DBName:    "this_database_does_not_exist_xyz",
		BackupDir: dir,
	}

	if err := r.Run(); err == nil {
		t.Fatal("expected error for non-existent database, got nil")
	}

	// Partial backup file must be cleaned up
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected cleanup of partial backup, found: %v", entries)
	}
}

// ── PostgreSQL ────────────────────────────────────────────────────────────────

func TestIntegration_Postgres_FetchDatabases(t *testing.T) {
	cfg := integrationPostgresConfig(t)
	r := &runner.BackupRunner{Config: cfg}

	dbs, err := r.FetchDatabases()
	if err != nil {
		t.Fatalf("FetchDatabases failed: %v", err)
	}
	if len(dbs) == 0 {
		t.Fatal("expected at least one database, got none")
	}
	// Maintenance databases should be excluded
	for _, db := range dbs {
		if db == "postgres" || db == "template0" || db == "template1" {
			t.Errorf("system database %q should be filtered out", db)
		}
	}
	t.Logf("Found databases: %v", dbs)
}

func TestIntegration_Postgres_Backup(t *testing.T) {
	cfg := integrationPostgresConfig(t)
	dbName := requireEnv(t, "TEST_PG_DB")
	dir := t.TempDir()

	r := &runner.BackupRunner{
		Config:    cfg,
		DBName:    dbName,
		BackupDir: dir,
	}

	if err := r.Run(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no backup file was created")
	}
	backupFile := filepath.Join(dir, entries[0].Name())
	info, _ := os.Stat(backupFile)
	if info.Size() == 0 {
		t.Errorf("backup file is empty: %s", backupFile)
	}
	t.Logf("Backup created: %s (%d bytes)", backupFile, info.Size())
}

// ── helpers ───────────────────────────────────────────────────────────────────

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	val := os.Getenv(key)
	if val == "" {
		t.Skipf("%s is not set — skipping integration test", key)
	}
	return val
}

func integrationMySQLConfig(t *testing.T) config.Config {
	t.Helper()
	host := requireEnv(t, "TEST_MYSQL_HOST")
	cfg := config.Config{}
	cfg.Database.Type = "mysql"
	cfg.Database.Host = host
	cfg.Database.User = requireEnv(t, "TEST_MYSQL_USER")
	cfg.Database.Password = requireEnv(t, "TEST_MYSQL_PASSWORD")
	cfg.Database.Port = config.DefaultMySQLPort
	cfg.Remote.MysqlBin = "mysql"
	cfg.Remote.MysqldumpBin = "mysqldump"
	cfg.Remote.PsqlBin = "psql"
	cfg.Remote.PgdumpBin = "pg_dump"
	return cfg
}

func integrationPostgresConfig(t *testing.T) config.Config {
	t.Helper()
	host := requireEnv(t, "TEST_PG_HOST")
	cfg := config.Config{}
	cfg.Database.Type = "postgres"
	cfg.Database.Host = host
	cfg.Database.User = requireEnv(t, "TEST_PG_USER")
	cfg.Database.Password = requireEnv(t, "TEST_PG_PASSWORD")
	cfg.Database.Port = config.DefaultPostgresPort
	cfg.Remote.MysqlBin = "mysql"
	cfg.Remote.MysqldumpBin = "mysqldump"
	cfg.Remote.PsqlBin = "psql"
	cfg.Remote.PgdumpBin = "pg_dump"
	return cfg
}
