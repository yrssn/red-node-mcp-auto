package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConsoleConfig(t *testing.T, values map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mysql.json")
	data, err := json.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

func clearConsoleDSNEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("MYSQL_DEMO_DSN", "")
}

func TestLoadConsoleDSNProductionPriority(t *testing.T) {
	clearConsoleDSNEnv(t)
	fileDSN := "console:file-secret@tcp(127.0.0.1:3306)/comment_console"
	envDSN := "console:env-secret@tcp(127.0.0.1:3306)/comment_console"
	path := writeConsoleConfig(t, map[string]string{"dsn": fileDSN})
	got, err := loadConsoleDSN(path, false)
	require.NoError(t, err)
	assert.Equal(t, fileDSN, got)

	t.Setenv("MYSQL_DSN", envDSN)
	got, err = loadConsoleDSN(path, false)
	require.NoError(t, err)
	assert.Equal(t, envDSN, got)
	got, err = loadConsoleDSN(filepath.Join(t.TempDir(), "missing.json"), false)
	require.NoError(t, err)
	assert.Equal(t, envDSN, got)
}

func TestLoadConsoleDSNDemoIsolation(t *testing.T) {
	clearConsoleDSNEnv(t)
	prodDSN := "console:prod-secret@tcp(127.0.0.1:3306)/comment_console"
	demoDSN := "console:demo-secret@tcp(127.0.0.1:3306)/comment_console_demo"
	path := writeConsoleConfig(t, map[string]string{"dsn": prodDSN, "demo_dsn": demoDSN})
	got, err := loadConsoleDSN(path, true)
	require.NoError(t, err)
	assert.Equal(t, demoDSN, got)

	t.Setenv("MYSQL_DSN", prodDSN)
	path = writeConsoleConfig(t, map[string]string{"dsn": prodDSN})
	_, err = loadConsoleDSN(path, true)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), prodDSN)
	assert.NotContains(t, err.Error(), "prod-secret")

	envDemo := "console:env-demo-secret@tcp(127.0.0.1:3306)/separate_demo"
	t.Setenv("MYSQL_DEMO_DSN", envDemo)
	got, err = loadConsoleDSN(path, true)
	require.NoError(t, err)
	assert.Equal(t, envDemo, got)
}

func TestLoadConsoleDSNErrorsDoNotLeakCredentials(t *testing.T) {
	clearConsoleDSNEnv(t)
	_, err := loadConsoleDSN(filepath.Join(t.TempDir(), "missing.json"), false)
	require.Error(t, err)

	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{\"dsn\":\"console:private-secret@tcp(localhost:3306)/console\""), 0600))
	_, err = loadConsoleDSN(path, false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private-secret")
	assert.NotContains(t, err.Error(), "console:private-secret")
}

func TestLoadConsoleDSNDemoRejectsSameDatabase(t *testing.T) {
	clearConsoleDSNEnv(t)
	path := writeConsoleConfig(t, map[string]string{
		"dsn":      "console:prod-private@tcp(localhost:3306)/comment_console?parseTime=true",
		"demo_dsn": "console:demo-private@tcp(127.0.0.1:3306)/comment_console?charset=utf8mb4",
	})
	_, err := loadConsoleDSN(path, true)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "prod-private")
	assert.NotContains(t, err.Error(), "demo-private")
}

func TestLoadConsoleDSNEnvOverridesBrokenFile(t *testing.T) {
	clearConsoleDSNEnv(t)
	path := filepath.Join(t.TempDir(), "broken.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0600))
	prod := "test:secret@tcp(localhost:3306)/console"
	demo := "test:secret@tcp(localhost:3306)/console_demo"
	t.Setenv("MYSQL_DSN", prod)
	got, err := loadConsoleDSN(path, false)
	require.NoError(t, err)
	assert.Equal(t, prod, got)
	t.Setenv("MYSQL_DEMO_DSN", demo)
	got, err = loadConsoleDSN(path, true)
	require.NoError(t, err)
	assert.Equal(t, demo, got)
}
