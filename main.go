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

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultPort     = 3306
	DirPerms        = 0755
	TimestampFormat = "2006-01-02_15-04-05"
)

// --- Configuration & Runner ---

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
	Remote struct {
		Type         string `toml:"type"`          // "local" (default), "docker", "kubernetes"
		Container    string `toml:"container"`     // Docker: container name; K8s: container name (optional)
		Namespace    string `toml:"namespace"`     // K8s: namespace (optional, uses current context)
		Pod          string `toml:"pod"`           // K8s: pod name
		MysqlBin     string `toml:"mysql_bin"`     // default: "mysql"
		MysqldumpBin string `toml:"mysqldump_bin"` // default: "mysqldump"
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

// buildRemoteCommand wraps a mysql/mysqldump command for docker or kubernetes execution.
// Password is injected via MYSQL_PWD environment variable (no temp file needed).
func (r *BackupRunner) buildRemoteCommand(executable string, mysqlArgs []string) (*exec.Cmd, error) {
	remote := r.Config.Remote
	switch strings.ToLower(remote.Type) {
	case "docker":
		if remote.Container == "" {
			return nil, fmt.Errorf("remote.container must be set for docker mode")
		}
		args := []string{"exec", "-e", "MYSQL_PWD=" + r.Config.Database.Password, remote.Container, executable}
		args = append(args, mysqlArgs...)
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
		// Use "env" inside the container to set MYSQL_PWD without shell quoting issues
		args = append(args, "--", "env", "MYSQL_PWD="+r.Config.Database.Password, executable)
		args = append(args, mysqlArgs...)
		return exec.Command("kubectl", args...), nil

	default:
		return nil, fmt.Errorf("unknown remote type: %q", remote.Type)
	}
}

func (r *BackupRunner) executeDump(destPath string) error {
	var cmd *exec.Cmd

	remoteType := strings.ToLower(r.Config.Remote.Type)
	if remoteType == "docker" || remoteType == "kubernetes" || remoteType == "k8s" {
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
		cmd = exec.Command("mysqldump", args...)
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
		return fmt.Errorf("mysqldump failed: %v, stderr: %s", err, stderr.String())
	}
	return nil
}

// FetchDatabases fetches the list of databases via the mysql CLI (local, docker or kubernetes)
func (r *BackupRunner) FetchDatabases() ([]string, error) {
	var cmd *exec.Cmd

	remoteType := strings.ToLower(r.Config.Remote.Type)
	if remoteType == "docker" || remoteType == "kubernetes" || remoteType == "k8s" {
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
		cmd = exec.Command("mysql", args...)
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch databases: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var dbs []string
	for _, line := range lines {
		// filter out system databases
		if line != "information_schema" && line != "performance_schema" && line != "" {
			dbs = append(dbs, line)
		}
	}
	return dbs, nil
}

// --- Bubble Tea TUI ---

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table     table.Model
	runner    *BackupRunner
	state     string // "selecting", "backing_up", "done", "error"
	err       error
	backupMsg string
}

type backupFinishedMsg struct {
	err error
}

func startBackup(r *BackupRunner) tea.Cmd {
	return func() tea.Msg {
		err := r.Run()
		return backupFinishedMsg{err: err}
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.state == "selecting" {
				selectedDB := m.table.SelectedRow()[0]
				m.runner.DBName = selectedDB
				m.state = "backing_up"
				return m, startBackup(m.runner)
			}
		}

	case backupFinishedMsg:
		if msg.err != nil {
			m.state = "error"
			m.err = msg.err
		} else {
			m.state = "done"
		}
		return m, tea.Quit
	}

	// Only update the table while in selection state
	if m.state == "selecting" {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m model) View() string {
	switch m.state {
	case "selecting":
		return baseStyle.Render(m.table.View()) + "\n  ↑/↓: Navigate • Enter: Start backup • q: Quit\n"
	case "backing_up":
		return fmt.Sprintf("\n  ⏳ Creating backup for database '%s'... Please wait.\n", m.runner.DBName)
	case "done":
		return fmt.Sprintf("\n  ✅ Backup for '%s' successfully created!\n", m.runner.DBName)
	case "error":
		return fmt.Sprintf("\n  ❌ Backup failed: %v\n", m.err)
	}
	return ""
}

// --- Main ---

func main() {
	dbName := flag.String("db", "", "Name of the database (if missing, TUI will open)")
	backupDir := flag.String("dir", "./backup", "Directory where the backup should be saved")
	configFile := flag.String("config", "./easy_sql_config.toml", "Path to the TOML configuration file")
	flag.Parse()

	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	runner := &BackupRunner{
		Config:    config,
		DBName:    *dbName,
		BackupDir: *backupDir,
	}

	// Case 1: CLI mode (scripting / cronjob)
	if *dbName != "" {
		fmt.Printf("⏳ Creating backup of database '%s'...\n", runner.DBName)
		if err := runner.Run(); err != nil {
			log.Fatalf("❌ Backup failed: %v\n", err)
		}
		fmt.Println("✅ Backup successfully created.")
		os.Exit(0)
	}

	// Case 2: Interactive mode (Bubble Tea TUI)
	fmt.Println("Connecting to database and loading tables...")
	dbs, err := runner.FetchDatabases()
	if err != nil {
		log.Fatalf("❌ Error fetching databases: %v", err)
	}

	if len(dbs) == 0 {
		fmt.Println("❌ No databases found (or insufficient privileges).")
		os.Exit(1)
	}

	// Build table
	columns := []table.Column{
		{Title: "Database", Width: 30},
	}
	var rows []table.Row
	for _, db := range dbs {
		rows = append(rows, table.Row{db})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(7),
	)

	// Table styling
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	// Start Bubble Tea program
	m := model{
		table:  t,
		runner: runner,
		state:  "selecting",
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
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

	if config.Database.Port == 0 {
		config.Database.Port = DefaultPort
	}
	if config.Remote.MysqlBin == "" {
		config.Remote.MysqlBin = "mysql"
	}
	if config.Remote.MysqldumpBin == "" {
		config.Remote.MysqldumpBin = "mysqldump"
	}
	return config, nil
}
