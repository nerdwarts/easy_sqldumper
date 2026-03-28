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
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
)

// runTUI shows the interactive multiselect and returns the selected database names.
// Returns an empty slice if the user cancelled or selected nothing.
func runTUI(dbs []string) ([]string, error) {
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
		return nil, err
	}

	return selected, nil
}

// runBackups runs a backup for each selected database and prints per-db status.
// Returns the names of any databases that failed.
func runBackups(config Config, backupDir string, selected []string) []string {
	fmt.Printf("\nStarting backup for %d database(s)...\n\n", len(selected))

	var failed []string
	for _, db := range selected {
		r := &BackupRunner{
			Config:    config,
			DBName:    db,
			BackupDir: backupDir,
		}
		fmt.Printf("⏳  %-30s", db)
		if err := r.Run(); err != nil {
			fmt.Printf("❌ failed: %v\n", err)
			failed = append(failed, db)
		} else {
			fmt.Println("✅ done")
		}
	}
	return failed
}

// runInteractive is the full interactive flow: fetch DBs → multiselect → backup.
func runInteractive(config Config, backupDir string) {
	runner := &BackupRunner{Config: config}

	fmt.Println("☁️🔌 Connecting to DBMS and loading databases...")
	dbs, err := runner.FetchDatabases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error fetching databases: %v\n", err)
		os.Exit(1)
	}
	if len(dbs) == 0 {
		fmt.Println("❌ No databases found (or insufficient privileges).")
		os.Exit(1)
	}

	selected, err := runTUI(dbs)
	if err != nil {
		// User cancelled with ctrl+c
		fmt.Println("Cancelled.")
		os.Exit(0)
	}
	if len(selected) == 0 {
		fmt.Println("No databases selected. Nothing to do.")
		os.Exit(0)
	}

	failed := runBackups(config, backupDir, selected)

	fmt.Println()
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "❌ %d backup(s) failed: %s\n", len(failed), strings.Join(failed, ", "))
		os.Exit(1)
	}
	fmt.Printf("✅ All %d backup(s) completed successfully.\n", len(selected))
}
