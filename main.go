package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Database struct {
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
}

func main() {
	dbName := flag.String("db", "", "Name of the database to be backed up (required)")
	backupDir := flag.String("dir", "./backup", "Directory where the backup should be saved")
	configFile := flag.String("config", "./dumper.toml", "Path to the TOML configuration file")

	flag.Parse()

	if *dbName == "" {
		fmt.Println("❌ Error: Please provide a database name.")
		fmt.Println("Example usage: ./sqldumper -db my_database")
		flag.Usage() // Shows help for all params
		os.Exit(1)
	}

	// 4. Load TOML config
	var config Config
	f, err := os.Open(*configFile)
	if err != nil {
		log.Fatalf("❌ Error opening config file '%s': %v\n", *configFile, err)
	}
	defer f.Close()

	if err := toml.NewDecoder(f).Decode(&config); err != nil {
		log.Fatalf("❌ Error parsing config file '%s': %v\n", *configFile, err)
	}

	// Set the default Port if not provided
	if config.Database.Port == 0 {
		config.Database.Port = 3306
	}

	if err := os.MkdirAll(*backupDir, 0755); err != nil {
		log.Fatalf("❌ Error creating backup directory '%s': %v\n", *backupDir, err)
	}

	timestampFormat := "2006-01-02_15-04-05"
	timestamp := time.Now().Format(timestampFormat) // Format: YYYY-MM-DD_HH-MM-SS
	fileName := fmt.Sprintf("%s_%s.sql", *dbName, timestamp)
	fullPath := filepath.Join(*backupDir, fileName)

	args := []string{
		fmt.Sprintf("--user=%s", config.Database.User),
		fmt.Sprintf("--password=%s", config.Database.Password),
		fmt.Sprintf("--host=%s", config.Database.Host),
		fmt.Sprintf("--port=%d", config.Database.Port),
	}
	if config.SSL.Enabled {
		args = append(args,
			fmt.Sprintf("--ssl-ca=%s", config.SSL.CA),
			fmt.Sprintf("--ssl-cert=%s", config.SSL.Cert),
			fmt.Sprintf("--ssl-key=%s", config.SSL.Key),
		)
		if config.SSL.VerifyServerCert {
			args = append(args, "--ssl-verify-server-cert")
		}
	}
	args = append(args, *dbName)

	cmd := exec.Command("mysqldump", args...)

	outFile, err := os.Create(fullPath)
	if err != nil {
		log.Fatalf("❌ Error creating backup file '%s': %v\n", fullPath, err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	fmt.Printf("⏳ Creating backup of database '%s'...\n", *dbName)
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Error creating backup: %v\n", err)
		// Delete the file if the backup fails to avoid leaving a partial backup
		err := os.Remove(fullPath)
		if err != nil {
			log.Printf("⚠️warning: failed to delete partial backup file: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Printf("✅ Backup successfully created: %s\n", fullPath)
}
