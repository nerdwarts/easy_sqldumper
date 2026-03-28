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
)

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

	// CLI flag overrides config file
	if *dbType != "" {
		config.Database.Type = *dbType
	}

	// Case 1: CLI mode (scripting / cronjob) — single database via -db flag
	if *dbName != "" {
		fmt.Printf("⏳ Creating backup of database '%s'...\n", *dbName)
		runner := &BackupRunner{
			Config:    config,
			DBName:    *dbName,
			BackupDir: *backupDir,
		}
		if err := runner.Run(); err != nil {
			log.Fatalf("❌ Backup failed: %v\n", err)
		}
		fmt.Println("✅ Backup successfully created.")
		os.Exit(0)
	}

	// Case 2: Interactive multiselect TUI
	runInteractive(config, *backupDir)
}
