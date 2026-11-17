package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleServesConfiguredFrontendDirectory(t *testing.T) {
	dir := newConsoleWebFixture(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "assets"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('fixture')"), 0600))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerConsoleWeb(router, dir)
	for _, item := range []struct{ path, body string }{
		{"/console/", "评论工作台测试"},
		{"/console/assets/app.js", "console.log('fixture')"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, item.path, nil))
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), item.body)
	}
}

func TestMissingFrontendDoesNotBlockAPI(t *testing.T) {
	app := NewAppServer(NewXiaohongshuService(), "")
	app.webDir = filepath.Join(t.TempDir(), "not-built")
	router := setupRoutes(app)
	for _, item := range []struct {
		path   string
		status int
	}{
		{"/health", http.StatusOK},
		{"/console/", http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, item.path, nil))
		assert.Equal(t, item.status, recorder.Code)
	}
}
