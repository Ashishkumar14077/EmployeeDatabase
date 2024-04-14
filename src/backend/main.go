package main

import (
	"backend/http_common"

	"github.com/gin-gonic/gin"
)

func main() {
	router := CreateGinRoutes()

	router.Run("localhost:8080")
}

func CreateGinRoutes() *gin.Engine {
	router := gin.Default()
	http_common.AddRoutes(router)
	return router
}
