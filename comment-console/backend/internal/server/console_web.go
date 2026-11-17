package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerConsoleWeb(router *gin.Engine, dir string) {
	router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/console/") })
	router.GET("/console", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/console/") })
	router.GET("/console/*path", gin.WrapH(http.StripPrefix("/console/", http.FileServer(http.Dir(dir)))))
}
