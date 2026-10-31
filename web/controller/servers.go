package controller

import (
	"strconv"

	"x-ui/database/model"
	"x-ui/web/service"

	"github.com/gin-gonic/gin"
)

type ServersController struct {
	BaseController

	service service.RemoteServerService
}

func NewServersController(g *gin.RouterGroup) *ServersController {
	a := &ServersController{}
	a.initRouter(g)
	return a
}

func (a *ServersController) initRouter(g *gin.RouterGroup) {
	g.GET("/list", a.list)
	g.GET("/localip", a.localIP)
	g.POST("/add", a.add)
	g.POST("/del/:id", a.del)
	g.POST("/check/:id", a.check)
	g.POST("/deploy/:id", a.deploy)
	g.POST("/link/:id", a.saveLink)
}

func (a *ServersController) list(c *gin.Context) {
	servers, err := a.service.GetAll()
	if servers == nil {
		servers = []*model.RemoteServer{}
	}
	jsonObj(c, servers, err)
}

func (a *ServersController) localIP(c *gin.Context) {
	jsonObj(c, gin.H{"ip": a.service.PublicIP()}, nil)
}

func (a *ServersController) add(c *gin.Context) {
	var server model.RemoteServer
	if err := c.ShouldBind(&server); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err := a.service.Add(&server)
	jsonMsg(c, "已添加", err)
}

func (a *ServersController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	jsonMsg(c, "已删除", a.service.Delete(id))
}

func (a *ServersController) check(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	result, err := a.service.Check(id)
	jsonObj(c, result, err)
}

type deployRelayForm struct {
	MasterHost string `json:"masterHost" form:"masterHost"`
	MasterPort int    `json:"masterPort" form:"masterPort"`
	RelayPort  int    `json:"relayPort" form:"relayPort"`
	Remark     string `json:"remark" form:"remark"`
}

func (a *ServersController) deploy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	var form deployRelayForm
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	result, err := a.service.DeployRelay(id, form.MasterHost, form.MasterPort, form.RelayPort, form.Remark)
	jsonObj(c, result, err)
}

func (a *ServersController) saveLink(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	var form struct {
		Link string `json:"link" form:"link"`
	}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, "参数错误", err)
		return
	}
	jsonMsg(c, "已保存", a.service.SetLastLink(id, form.Link))
}
