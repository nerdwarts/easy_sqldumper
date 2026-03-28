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

package secrets

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SecretsConfig holds provider-specific connection settings.
// These are read from the [secrets] section of the config file.
type SecretsConfig struct {
	Vault   VaultConfig   `toml:"vault"`
	Doppler DopplerConfig `toml:"doppler"`
}

// VaultConfig contains connection details for HashiCorp Vault or OpenBao.
// OpenBao is API-compatible with Vault — just point address at your OpenBao server.
type VaultConfig struct {
	Address string `toml:"address"` // e.g. "https://vault.example.com:8200"
	Token   string `toml:"token"`   // literal token or "env:VAULT_TOKEN"
}

// DopplerConfig contains connection details for Doppler.
type DopplerConfig struct {
	Token   string `toml:"token"`   // literal token or "env:DOPPLER_TOKEN"
	Project string `toml:"project"` // Doppler project name
	Config  string `toml:"config"`  // Doppler config / environment name
}

var secretsHTTPClient = &http.Client{Timeout: 10 * time.Second}

// ResolveSecret resolves a secret value from the configured backend.
//
// Supported prefixes:
//
//	env:VAR_NAME              → OS environment variable
//	file:/path/to/file        → file contents (Docker / K8s secret mount)
//	vault:mount/path#field    → HashiCorp Vault or OpenBao KV v2
//	doppler:SECRET_NAME       → Doppler
//	(no prefix)               → literal value (fully backward compatible)
func ResolveSecret(value string, sc SecretsConfig) (string, error) {
	switch {
	case strings.HasPrefix(value, "env:"):
		return resolveEnv(strings.TrimPrefix(value, "env:"))
	case strings.HasPrefix(value, "file:"):
		return resolveFile(strings.TrimPrefix(value, "file:"))
	case strings.HasPrefix(value, "vault:"):
		return resolveVault(strings.TrimPrefix(value, "vault:"), sc.Vault)
	case strings.HasPrefix(value, "doppler:"):
		return resolveDoppler(strings.TrimPrefix(value, "doppler:"), sc.Doppler)
	default:
		// Literal value — no change. Backward compatible.
		return value, nil
	}
}

// resolveEnv reads a secret from an OS environment variable.
func resolveEnv(name string) (string, error) {
	val, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return val, nil
}

// resolveFile reads a secret from a file path.
// Trailing newlines are trimmed (standard for Docker/K8s secret mounts).
func resolveFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret file %q: %w", path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// resolveVault fetches a secret from HashiCorp Vault or OpenBao (KV v2).
//
// value format: "mount/path/to/secret#field"
// Example:      "secret/data/myapp#db_password"
//
// The Vault token is resolved from secrets.vault.token (which itself may be
// "env:VAULT_TOKEN") or from the VAULT_TOKEN environment variable as a fallback.
func resolveVault(value string, cfg VaultConfig) (string, error) {
	if cfg.Address == "" {
		return "", fmt.Errorf(
			"secrets.vault.address is required for vault: references " +
				"(e.g. https://vault.example.com:8200)")
	}

	// Split "mount/path/to/secret#field" into path and field
	path, field, ok := strings.Cut(value, "#")
	if !ok || field == "" {
		return "", fmt.Errorf(
			"vault reference must include a field name: "+
				"\"vault:mount/path#field\", got %q", value)
	}

	token, err := resolveTokenField(cfg.Token, "VAULT_TOKEN")
	if err != nil {
		return "", fmt.Errorf("resolving vault token: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s", strings.TrimRight(cfg.Address, "/"), path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)

	resp, err := secretsHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault request to %q failed: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vault returned HTTP %d for path %q: %s",
			resp.StatusCode, path, strings.TrimSpace(string(body)))
	}

	// Vault KV v2 response: { "data": { "data": { "field": "value" } } }
	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding vault response: %w", err)
	}

	raw, ok := result.Data.Data[field]
	if !ok {
		return "", fmt.Errorf("field %q not found in vault secret at path %q", field, path)
	}
	str, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("vault field %q at path %q is not a string", field, path)
	}
	return str, nil
}

// resolveDoppler fetches a single secret from the Doppler API.
//
// value: the Doppler secret name (e.g. "DB_PASSWORD")
//
// The Doppler token is resolved from secrets.doppler.token (which itself may
// be "env:DOPPLER_TOKEN") or from the DOPPLER_TOKEN environment variable as
// a fallback.
func resolveDoppler(secretName string, cfg DopplerConfig) (string, error) {
	if cfg.Project == "" || cfg.Config == "" {
		return "", fmt.Errorf(
			"secrets.doppler.project and secrets.doppler.config are required " +
				"for doppler: references")
	}

	token, err := resolveTokenField(cfg.Token, "DOPPLER_TOKEN")
	if err != nil {
		return "", fmt.Errorf("resolving doppler token: %w", err)
	}

	url := fmt.Sprintf(
		"https://api.doppler.com/v3/configs/config/secret?project=%s&config=%s&name=%s",
		cfg.Project, cfg.Config, secretName)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building doppler request: %w", err)
	}
	// Doppler uses HTTP Basic Auth: token as username, empty password
	req.SetBasicAuth(token, "")

	resp, err := secretsHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("doppler request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("doppler returned HTTP %d for secret %q: %s",
			resp.StatusCode, secretName, strings.TrimSpace(string(body)))
	}

	// Doppler single-secret response: { "secret": { "value": { "raw": "..." } } }
	var result struct {
		Secret struct {
			Value struct {
				Raw string `json:"raw"`
			} `json:"value"`
		} `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding doppler response: %w", err)
	}

	return result.Secret.Value.Raw, nil
}

// resolveTokenField resolves an API token value.
// The token field itself may be an "env:VAR" reference; if it is empty the
// fallback environment variable is checked.
func resolveTokenField(tokenField, fallbackEnvVar string) (string, error) {
	if strings.HasPrefix(tokenField, "env:") {
		return resolveEnv(strings.TrimPrefix(tokenField, "env:"))
	}
	if tokenField != "" {
		return tokenField, nil
	}
	// Fall back to the well-known env var for this provider
	val := os.Getenv(fallbackEnvVar)
	if val == "" {
		return "", fmt.Errorf(
			"token is not set — provide secrets.vault/doppler.token or set %s",
			fallbackEnvVar)
	}
	return val, nil
}
