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
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"easy_sqldumper/internal/config"
	"easy_sqldumper/internal/runner"
	"easy_sqldumper/internal/tui"
)

func main() {
	// Subcommand routing — must happen before flag.Parse()
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runInit(os.Args[2:])
		return
	}

	defaultConfig := defaultConfigPath()

	dbName := flag.String("db", "", "Name of the database (if missing, interactive multiselect opens)")
	backupDir := flag.String("dir", "./backup", "Directory where the backup should be saved")
	configFile := flag.String("config", defaultConfig, "Path to the TOML configuration file")
	dbType := flag.String("type", "", "Database type: \"mysql\" or \"postgres\" (overrides config)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	// CLI flag overrides config file
	if *dbType != "" {
		cfg.Database.Type = *dbType
	}

	// Case 1: CLI mode (scripting / cronjob) — single database via -db flag
	if *dbName != "" {
		fmt.Printf("⏳ Creating backup of database '%s'...\n", *dbName)
		r := &runner.BackupRunner{
			Config:    cfg,
			DBName:    *dbName,
			BackupDir: *backupDir,
		}
		if err := r.Run(); err != nil {
			log.Fatalf("❌ Backup failed: %v\n", err)
		}
		fmt.Println("✅ Backup successfully created.")
		os.Exit(0)
	}

	// Case 2: Interactive multiselect TUI
	tui.RunInteractive(cfg, *backupDir)
}

// defaultConfigPath returns the path to easy_sql_config.toml located in the
// same directory as the running binary. If the executable path cannot be
// resolved, it falls back to the current working directory.
func defaultConfigPath() string {
	exe, err := resolvedExecutable()
	if err != nil {
		return "./.easy_sql_config.toml"
	}
	return filepath.Join(filepath.Dir(exe), ".easy_sql_config.toml")
}

// resolvedExecutable returns the absolute, symlink-resolved path of the
// running binary.
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
