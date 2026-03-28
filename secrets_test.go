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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ── resolveSecret dispatcher ──────────────────────────────────────────────────

func TestResolveSecret_Literal(t *testing.T) {
	got, err := resolveSecret("mysecret", SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mysecret" {
		t.Errorf("expected 'mysecret', got %q", got)
	}
}

func TestResolveSecret_Empty(t *testing.T) {
	got, err := resolveSecret("", SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ── resolveEnv ────────────────────────────────────────────────────────────────

func TestResolveSecret_Env_Set(t *testing.T) {
	t.Setenv("SQLDUMPER_TEST_SECRET", "hello-from-env")
	got, err := resolveSecret("env:SQLDUMPER_TEST_SECRET", SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello-from-env" {
		t.Errorf("expected 'hello-from-env', got %q", got)
	}
}

func TestResolveSecret_Env_NotSet(t *testing.T) {
	os.Unsetenv("SQLDUMPER_DEFINITELY_NOT_SET_XYZ")
	_, err := resolveSecret("env:SQLDUMPER_DEFINITELY_NOT_SET_XYZ", SecretsConfig{})
	if err == nil {
		t.Fatal("expected error for unset env var, got nil")
	}
	if !strings.Contains(err.Error(), "is not set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ── resolveFile ───────────────────────────────────────────────────────────────

func TestResolveSecret_File_Exists(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "secret-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("file-secret")
	f.Close()

	got, err := resolveSecret("file:"+f.Name(), SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "file-secret" {
		t.Errorf("expected 'file-secret', got %q", got)
	}
}

func TestResolveSecret_File_NotFound(t *testing.T) {
	_, err := resolveSecret("file:/nonexistent/secret.txt", SecretsConfig{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestResolveSecret_File_TrimsTrailingNewlines(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "secret-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("mypassword\n")
	f.Close()

	got, err := resolveSecret("file:"+f.Name(), SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mypassword" {
		t.Errorf("expected 'mypassword' (without newline), got %q", got)
	}
}

func TestResolveSecret_File_TrimsWindowsCRLF(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "secret-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("mypassword\r\n")
	f.Close()

	got, err := resolveSecret("file:"+f.Name(), SecretsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mypassword" {
		t.Errorf("expected 'mypassword' (without CRLF), got %q", got)
	}
}

// ── resolveVault ──────────────────────────────────────────────────────────────

func TestResolveSecret_Vault_NoAddress(t *testing.T) {
	_, err := resolveSecret("vault:secret/data/myapp#password", SecretsConfig{})
	if err == nil {
		t.Fatal("expected error for missing vault address, got nil")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveSecret_Vault_NoFieldSeparator(t *testing.T) {
	sc := SecretsConfig{Vault: VaultConfig{Address: "http://vault:8200", Token: "tok"}}
	_, err := resolveSecret("vault:secret/data/myapp", sc)
	if err == nil {
		t.Fatal("expected error for missing # field separator, got nil")
	}
	if !strings.Contains(err.Error(), "field name") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveSecret_Vault_Success(t *testing.T) {
	// Mock Vault KV v2 server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"db_password": "vault-secret-value",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sc := SecretsConfig{
		Vault: VaultConfig{Address: srv.URL, Token: "test-token"},
	}
	got, err := resolveSecret("vault:secret/data/myapp#db_password", sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "vault-secret-value" {
		t.Errorf("expected 'vault-secret-value', got %q", got)
	}
}

func TestResolveSecret_Vault_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission denied", http.StatusForbidden)
	}))
	defer srv.Close()

	sc := SecretsConfig{
		Vault: VaultConfig{Address: srv.URL, Token: "bad-token"},
	}
	_, err := resolveSecret("vault:secret/data/myapp#password", sc)
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

func TestResolveSecret_Vault_MissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"other_field": "value",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sc := SecretsConfig{
		Vault: VaultConfig{Address: srv.URL, Token: "tok"},
	}
	_, err := resolveSecret("vault:secret/data/myapp#db_password", sc)
	if err == nil {
		t.Fatal("expected error for missing field, got nil")
	}
	if !strings.Contains(err.Error(), "db_password") {
		t.Errorf("expected field name in error, got: %v", err)
	}
}

func TestResolveSecret_Vault_TokenFromEnv(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "env-vault-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "env-vault-token" {
			http.Error(w, "wrong token", http.StatusForbidden)
			return
		}
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{"pass": "ok"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Token field is empty — should fall back to VAULT_TOKEN env var
	sc := SecretsConfig{
		Vault: VaultConfig{Address: srv.URL, Token: ""},
	}
	got, err := resolveSecret("vault:secret/data/app#pass", sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("expected 'ok', got %q", got)
	}
}

// ── resolveDoppler ────────────────────────────────────────────────────────────

func TestResolveSecret_Doppler_MissingProjectOrConfig(t *testing.T) {
	sc := SecretsConfig{
		Doppler: DopplerConfig{Token: "tok", Project: "", Config: ""},
	}
	_, err := resolveSecret("doppler:DB_PASSWORD", sc)
	if err == nil {
		t.Fatal("expected error for missing project/config, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveSecret_Doppler_Success(t *testing.T) {
	// Mock Doppler API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := r.BasicAuth()
		if !ok || user != "test-doppler-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]interface{}{
			"secret": map[string]interface{}{
				"value": map[string]interface{}{
					"raw": "doppler-secret-value",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Override the HTTP client to point at our mock server
	original := secretsHTTPClient
	secretsHTTPClient = srv.Client()
	defer func() { secretsHTTPClient = original }()

	// We can't override the Doppler URL easily since it's hard-coded,
	// so we test resolveDoppler directly with a custom transport instead.
	// Use the test server URL by patching the client transport.
	secretsHTTPClient = &http.Client{
		Transport: redirectTransport(srv.URL),
	}

	sc := SecretsConfig{
		Doppler: DopplerConfig{
			Token:   "test-doppler-token",
			Project: "my-project",
			Config:  "production",
		},
	}
	got, err := resolveDoppler("DB_PASSWORD", sc.Doppler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "doppler-secret-value" {
		t.Errorf("expected 'doppler-secret-value', got %q", got)
	}
}

func TestResolveSecret_Doppler_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	original := secretsHTTPClient
	secretsHTTPClient = &http.Client{Transport: redirectTransport(srv.URL)}
	defer func() { secretsHTTPClient = original }()

	sc := DopplerConfig{
		Token:   "bad-token",
		Project: "proj",
		Config:  "prod",
	}
	_, err := resolveDoppler("DB_PASSWORD", sc)
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

// ── resolveTokenField ─────────────────────────────────────────────────────────

func TestResolveTokenField_Literal(t *testing.T) {
	got, err := resolveTokenField("my-literal-token", "FALLBACK_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-literal-token" {
		t.Errorf("expected 'my-literal-token', got %q", got)
	}
}

func TestResolveTokenField_EnvRef(t *testing.T) {
	t.Setenv("SQLDUMPER_TOKEN_TEST", "from-env")
	got, err := resolveTokenField("env:SQLDUMPER_TOKEN_TEST", "FALLBACK_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from-env" {
		t.Errorf("expected 'from-env', got %q", got)
	}
}

func TestResolveTokenField_FallbackEnv(t *testing.T) {
	t.Setenv("SQLDUMPER_FALLBACK_TOKEN", "fallback-value")
	got, err := resolveTokenField("", "SQLDUMPER_FALLBACK_TOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fallback-value" {
		t.Errorf("expected 'fallback-value', got %q", got)
	}
}

func TestResolveTokenField_NothingSet_Error(t *testing.T) {
	os.Unsetenv("SQLDUMPER_MISSING_TOKEN_XYZ")
	_, err := resolveTokenField("", "SQLDUMPER_MISSING_TOKEN_XYZ")
	if err == nil {
		t.Fatal("expected error when token is not set anywhere, got nil")
	}
}

// ── redirectTransport ─────────────────────────────────────────────────────────

// redirectTransport is a test helper that rewrites all HTTP requests to point
// at a given base URL (used to redirect Doppler API calls to a local mock).
type redirectTransport string

func (base redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(string(base), "http://")
	return http.DefaultTransport.RoundTrip(req2)
}
