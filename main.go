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

const (
	DefaultPort     = 3306
	DirPerms        = 0755
	TimestampFormat = "2006-01-02_15-04-05"
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

	fmt.Printf("⏳ Creating backup of database '%s'...\n", r.DBName)
	if dumpErr := r.executeDump(fullPath); dumpErr != nil {
		// Delete the file if the backup fails to avoid leaving a partial backup
		err := os.Remove(fullPath)
		if err != nil {
			log.Printf("⚠️ Warning: failed to delete partial backup file: %v", err)
		}
		return dumpErr
	}

	fmt.Printf("✅ Backup successfully created: %s\n", fullPath)
	return nil
}

func (r *BackupRunner) generateFilePath() string {
	timestamp := time.Now().Format(TimestampFormat)
	fileName := fmt.Sprintf("%s_%s.sql", r.DBName, timestamp)
	return filepath.Join(r.BackupDir, fileName)
}

func (r *BackupRunner) executeDump(destPath string) error {
	args := []string{
		fmt.Sprintf("--user=%s", r.Config.Database.User),
		fmt.Sprintf("--password=%s", r.Config.Database.Password),
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
	args = append(args, r.DBName)

	cmd := exec.Command("mysqldump", args...)

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("error creating backup file '%s': %w", destPath, err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump failed: %w", err)
	}

	return nil
}

func main() {
	dbName := flag.String("db", "", "Name of the database to be backed up (required)")
	backupDir := flag.String("dir", "./backup", "Directory where the backup should be saved")
	configFile := flag.String("config", "./dumper.toml", "Path to the TOML configuration file")
	flag.Parse()

	if *dbName == "" {
		fmt.Fprintln(os.Stderr, "❌ Error: Please provide a database name.")
		fmt.Println("Example usage: ./sqldumper -db my_database")
		flag.Usage() // Shows help for all params
		os.Exit(1)
	}

	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	runner := &BackupRunner{
		Config:    config,
		DBName:    *dbName,
		BackupDir: *backupDir,
	}

	if err := runner.Run(); err != nil {
		log.Fatalf("❌ Backup failed: %v\n", err)
	}
}

func loadConfig(path string) (Config, error) {
	var config Config

	f, err := os.Open(path)
	if err != nil {
		return config, fmt.Errorf("error opening config file %q: %w", path, err)
	}
	defer f.Close()

	if err := toml.NewDecoder(f).Decode(&config); err != nil {
		return config, fmt.Errorf("error parsing config file %q: %w", path, err)
	}

	if config.Database.Port == 0 {
		config.Database.Port = DefaultPort
	}

	return config, nil
}
