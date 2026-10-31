package controller

import (
	"x-ui/web/service"

	"github.com/gin-gonic/gin"
)

type APIController struct {
	BaseController
	inboundController *InboundController
	serverController  *ServerController
	serversController *ServersController
	Tgbot             service.Tgbot
	serverService     service.ServerService
}

func NewAPIController(g *gin.RouterGroup) *APIController {
	a := &APIController{}
	a.initRouter(g)
	return a
}

func (a *APIController) initRouter(g *gin.RouterGroup) {
	// Main API group
	api := g.Group("/panel/api")
	api.Use(a.checkLogin)

	// Inbounds API
	inbounds := api.Group("/inbounds")
	a.inboundController = NewInboundController(inbounds)

	// Server API
	server := api.Group("/server")
	a.serverController = NewServerController(server, a.serverService)

	// 主从管理 API（被控端 / 中转机）
	servers := api.Group("/servers")
	a.serversController = NewServersController(servers)

	// Extra routes
	api.GET("/backuptotgbot", a.BackuptoTgbot)
}

func (a *APIController) BackuptoTgbot(c *gin.Context) {
	a.Tgbot.SendBackupToAdmins()
}
