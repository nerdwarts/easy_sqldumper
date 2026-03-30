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
	"os"
	"path/filepath"
	"strings"
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	shellFlag := fs.String("shell", "", "Target shell: bash, fish, nu (default: auto-detect from $SHELL)")
	noAlias := fs.Bool("no-alias", false, "Skip creating the 'esd' alias")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: easy_sqldumper init [--shell bash|fish|nu] [--no-alias]")
		fmt.Fprintln(os.Stderr, "\nAdds the binary to PATH and creates an 'esd' alias in your shell config.")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	exe, err := resolvedExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Cannot resolve binary path: %v\n", err)
		os.Exit(1)
	}
	binDir := filepath.Dir(exe)

	shell := *shellFlag
	if shell == "" {
		shell = detectShell()
	}
	if shell == "" {
		fmt.Fprintln(os.Stderr, "❌ Cannot detect shell — use --shell bash|fish|nu")
		os.Exit(1)
	}

	fmt.Println("🔧 easy_sqldumper init")
	fmt.Printf("   Binary : %s\n", exe)
	fmt.Printf("   Shell  : %s\n\n", shell)

	switch strings.ToLower(shell) {
	case "bash":
		initBash(exe, binDir, *noAlias)
	case "fish":
		initFish(exe, binDir, *noAlias)
	case "nu", "nushell":
		initNushell(exe, binDir, *noAlias)
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown shell %q — use --shell bash|fish|nu\n", shell)
		os.Exit(1)
	}
}

// ── shell detection ───────────────────────────────────────────────────────────

func detectShell() string {
	// fish exposes its own version variable — most reliable signal
	if os.Getenv("FISH_VERSION") != "" {
		return "fish"
	}
	// Nushell sets this in its environment
	if os.Getenv("NU_VERSION") != "" {
		return "nu"
	}
	base := filepath.Base(os.Getenv("SHELL"))
	switch {
	case strings.Contains(base, "fish"):
		return "fish"
	case strings.Contains(base, "bash"):
		return "bash"
	case base == "nu":
		return "nu"
	}
	return ""
}

// ── per-shell init ────────────────────────────────────────────────────────────

func initBash(exe, binDir string, noAlias bool) {
	cfg := bashConfigFile()
	sourced := false

	added, err := appendIfMissing(cfg,
		"# easy_sqldumper — added by 'easy_sqldumper init'",
		fmt.Sprintf(`export PATH="%s:$PATH"`, binDir),
	)
	mustOK(err)
	if added {
		fmt.Printf("→ Added %s to PATH in %s\n", binDir, cfg)
		sourced = true
	} else {
		fmt.Printf("✓ %s is already in PATH\n", binDir)
	}

	if !noAlias {
		added, err = appendIfMissing(cfg,
			"# easy_sqldumper — alias",
			fmt.Sprintf(`alias esd="%s"`, exe),
		)
		mustOK(err)
		if added {
			fmt.Printf("→ Added alias 'esd' in %s\n", cfg)
			sourced = true
		} else {
			fmt.Printf("✓ alias 'esd' already defined\n")
		}
	}

	if sourced {
		fmt.Printf("\nRestart your shell or run:\n  source %s\n", cfg)
	}
}

func initFish(exe, binDir string, noAlias bool) {
	cfg := fishConfigFile()
	sourced := false

	// fish_add_path is idempotent by design (fish ≥ 3.2)
	added, err := appendIfMissing(cfg,
		"# easy_sqldumper — added by 'easy_sqldumper init'",
		fmt.Sprintf("fish_add_path %q", binDir),
	)
	mustOK(err)
	if added {
		fmt.Printf("→ Added %s to PATH in %s\n", binDir, cfg)
		sourced = true
	} else {
		fmt.Printf("✓ %s is already in PATH\n", binDir)
	}

	if !noAlias {
		added, err = appendIfMissing(cfg,
			"# easy_sqldumper — alias",
			fmt.Sprintf("alias esd=%q", exe),
		)
		mustOK(err)
		if added {
			fmt.Printf("→ Added alias 'esd' in %s\n", cfg)
			sourced = true
		} else {
			fmt.Printf("✓ alias 'esd' already defined\n")
		}
	}

	if sourced {
		fmt.Printf("\nRestart your shell or run:\n  source %s\n", cfg)
	}
}

func initNushell(exe, binDir string, noAlias bool) {
	dir := nushellConfigDir()
	envFile := filepath.Join(dir, "env.nu")
	cfgFile := filepath.Join(dir, "config.nu")
	needsRestart := false

	added, err := appendIfMissing(envFile,
		"# easy_sqldumper — added by 'easy_sqldumper init'",
		fmt.Sprintf("$env.PATH = ($env.PATH | prepend %q)", binDir),
	)
	mustOK(err)
	if added {
		fmt.Printf("→ Added %s to PATH in %s\n", binDir, envFile)
		needsRestart = true
	} else {
		fmt.Printf("✓ %s is already in PATH\n", binDir)
	}

	if !noAlias {
		added, err = appendIfMissing(cfgFile,
			"# easy_sqldumper — alias",
			fmt.Sprintf("alias esd = %s", exe),
		)
		mustOK(err)
		if added {
			fmt.Printf("→ Added alias 'esd' in %s\n", cfgFile)
			needsRestart = true
		} else {
			fmt.Printf("✓ alias 'esd' already defined\n")
		}
	}

	if needsRestart {
		fmt.Println("\nRestart your shell to apply the changes.")
	}
}

// ── config file paths ─────────────────────────────────────────────────────────

func bashConfigFile() string {
	home := userHome()
	// Prefer .bashrc (Linux default); fall back to .bash_profile (macOS default)
	rc := filepath.Join(home, ".bashrc")
	if _, err := os.Stat(rc); err == nil {
		return rc
	}
	return filepath.Join(home, ".bash_profile")
}

func fishConfigFile() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".config")
	}
	return filepath.Join(base, "fish", "config.fish")
}

func nushellConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".config")
	}
	return filepath.Join(base, "nushell")
}

func userHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// ── helpers ───────────────────────────────────────────────────────────────────

// appendIfMissing appends a comment + line to file unless the line is already
// present. Creates the file and any missing parent directories if needed.
// Returns true when something was actually written.
func appendIfMissing(file, comment, line string) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", file, err)
	}
	if strings.Contains(string(data), line) {
		return false, nil // already present — nothing to do
	}
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return false, fmt.Errorf("creating parent directory for %s: %w", file, err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, fmt.Errorf("opening %s for writing: %w", file, err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n%s\n%s\n", comment, line)
	return true, err
}

func mustOK(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}
