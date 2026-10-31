package service

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"x-ui/database"
	"x-ui/database/model"
)

// RemoteServerService 主从管理：被控端面板 / 中转机
type RemoteServerService struct{}

type remoteResp struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

func normalizePanelUrl(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimSuffix(u, "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

func newPanelClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}, nil
}

// login 登录被控端面板，成功返回已带 Cookie 的 client
func (s *RemoteServerService) login(server *model.RemoteServer) (*http.Client, error) {
	client, err := newPanelClient()
	if err != nil {
		return nil, err
	}
	base := normalizePanelUrl(server.Url)

	form := url.Values{}
	form.Set("username", server.Username)
	form.Set("password", server.Password)
	resp, err := client.PostForm(base+"/login", form)
	if err != nil {
		return nil, fmt.Errorf("连接被控端失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var r remoteResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("被控端返回数据异常(HTTP %d)，请检查面板地址与访问路径", resp.StatusCode)
	}
	if !r.Success {
		if r.Msg == "" {
			r.Msg = "用户名或密码错误"
		}
		return nil, fmt.Errorf("登录被控端失败: %s", r.Msg)
	}
	return client, nil
}

func (s *RemoteServerService) GetAll() ([]*model.RemoteServer, error) {
	var servers []*model.RemoteServer
	err := database.GetDB().Order("id asc").Find(&servers).Error
	return servers, err
}

func (s *RemoteServerService) Get(id int) (*model.RemoteServer, error) {
	if id <= 0 {
		return nil, fmt.Errorf("无效的服务器 id")
	}
	var server model.RemoteServer
	err := database.GetDB().First(&server, id).Error
	if err != nil {
		return nil, err
	}
	return &server, nil
}

func (s *RemoteServerService) Add(server *model.RemoteServer) error {
	if strings.TrimSpace(server.Name) == "" {
		return fmt.Errorf("请填写备注名称")
	}
	if strings.TrimSpace(server.Url) == "" {
		return fmt.Errorf("请填写面板地址")
	}
	if strings.TrimSpace(server.Username) == "" {
		return fmt.Errorf("请填写面板用户名")
	}
	server.Url = normalizePanelUrl(server.Url)
	server.CreatedAt = time.Now().Unix()
	return database.GetDB().Create(server).Error
}

func (s *RemoteServerService) Delete(id int) error {
	return database.GetDB().Delete(&model.RemoteServer{}, id).Error
}

func (s *RemoteServerService) SetLastLink(id int, link string) error {
	return database.GetDB().Model(&model.RemoteServer{}).Where("id = ?", id).
		Update("last_link", link).Error
}

// Check 检测被控端是否可用，并回写状态
func (s *RemoteServerService) Check(id int) (map[string]any, error) {
	server, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	client, err := s.login(server)
	result := map[string]any{
		"id":    server.Id,
		"name":  server.Name,
		"ok":    false,
		"msg":   "",
		"ms":    0,
		"count": 0,
	}
	if err != nil {
		result["msg"] = err.Error()
		database.GetDB().Model(&model.RemoteServer{}).Where("id = ?", id).
			Updates(map[string]any{"status": false, "last_check": time.Now().Unix()})
		return result, nil
	}

	resp, err := client.Get(normalizePanelUrl(server.Url) + "/panel/api/inbounds/list")
	if err != nil {
		result["msg"] = fmt.Sprintf("读取被控端入站列表失败: %v", err)
		database.GetDB().Model(&model.RemoteServer{}).Where("id = ?", id).
			Updates(map[string]any{"status": false, "last_check": time.Now().Unix()})
		return result, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r remoteResp
	_ = json.Unmarshal(body, &r)
	count := 0
	if len(r.Obj) > 0 {
		var list []any
		if err := json.Unmarshal(r.Obj, &list); err == nil {
			count = len(list)
		}
	}
	ok := r.Success
	msg := "连接正常"
	if !ok {
		msg = r.Msg
		if msg == "" {
			msg = "被控端接口返回异常"
		}
	}
	result["ok"] = ok
	result["msg"] = msg
	result["ms"] = time.Since(start).Milliseconds()
	result["count"] = count
	database.GetDB().Model(&model.RemoteServer{}).Where("id = ?", id).
		Updates(map[string]any{"status": ok, "last_check": time.Now().Unix()})
	return result, nil
}

// DeployRelay 在被控端(中转机)上创建 dokodemo-door 入站，把流量转发回主控端入站端口
func (s *RemoteServerService) DeployRelay(id int, masterHost string, masterPort int, relayPort int, remark string) (map[string]any, error) {
	server, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if masterPort <= 0 || masterPort > 65535 {
		return nil, fmt.Errorf("主控端端口无效")
	}
	if relayPort <= 0 || relayPort > 65535 {
		return nil, fmt.Errorf("中转端口无效")
	}
	if strings.TrimSpace(masterHost) == "" {
		return nil, fmt.Errorf("主控端地址不能为空")
	}
	if remark == "" {
		remark = fmt.Sprintf("X-Panel中转 -> %s:%d", masterHost, masterPort)
	}

	client, err := s.login(server)
	if err != nil {
		return nil, err
	}

	settings := map[string]any{
		"address": masterHost,
		"port":    masterPort,
		"network": "tcp,udp",
	}
	settingsBytes, _ := json.Marshal(settings)
	stream := map[string]any{
		"network":  "tcp",
		"security": "none",
		"tcpSettings": map[string]any{
			"acceptProxyProtocol": false,
			"header":              map[string]any{"type": "none"},
		},
	}
	streamBytes, _ := json.Marshal(stream)
	sniffingBytes, _ := json.Marshal(map[string]any{"enabled": false})

	inbound := map[string]any{
		"up":             0,
		"down":           0,
		"total":          0,
		"remark":         remark,
		"enable":         true,
		"expiryTime":     0,
		"listen":         "",
		"port":           relayPort,
		"protocol":       "dokodemo-door",
		"settings":       string(settingsBytes),
		"streamSettings": string(streamBytes),
		"sniffing":       string(sniffingBytes),
	}
	payload, _ := json.Marshal(inbound)

	req, err := http.NewRequest(http.MethodPost, normalizePanelUrl(server.Url)+"/panel/api/inbounds/add", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("创建中转入站失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r remoteResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("被控端返回异常(HTTP %d)", resp.StatusCode)
	}
	if !r.Success {
		msg := r.Msg
		if msg == "" {
			msg = "被控端拒绝了中转入站配置"
		}
		return nil, fmt.Errorf("创建中转入站失败: %s", msg)
	}

	relayHost := "127.0.0.1"
	if u, err := url.Parse(normalizePanelUrl(server.Url)); err == nil {
		relayHost = u.Hostname()
	}
	return map[string]any{
		"host":   relayHost,
		"port":   relayPort,
		"msg":    "中转入站已创建",
		"remark": remark,
	}, nil
}

// PublicIP 返回本机公网 IP，用于中转配置的默认回连地址
func (s *RemoteServerService) PublicIP() string {
	ip := getPublicIP("https://api.ipify.org")
	if ip == "N/A" || ip == "" {
		ip = getPublicIP("https://ipv4.icanhazip.com")
	}
	if ip == "" {
		ip = "N/A"
	}
	return ip
}

// RemoteClient 已登录的被控端面板客户端（Bot 多面板切换用）
type RemoteClient struct {
	base string
	http *http.Client
}

// ClientFor 用保存的用户名密码登录指定被控端，返回可用客户端
func (s *RemoteServerService) ClientFor(server *model.RemoteServer) (*RemoteClient, error) {
	if server == nil {
		return nil, fmt.Errorf("服务器不存在")
	}
	client, err := s.login(server)
	if err != nil {
		return nil, err
	}
	return &RemoteClient{base: normalizePanelUrl(server.Url), http: client}, nil
}

func (c *RemoteClient) getJSON(path string, out any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r remoteResp
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("被控端返回异常(HTTP %d)", resp.StatusCode)
	}
	if !r.Success {
		msg := r.Msg
		if msg == "" {
			msg = "被控端接口返回失败"
		}
		return fmt.Errorf("%s", msg)
	}
	if out != nil && len(r.Obj) > 0 {
		return json.Unmarshal(r.Obj, out)
	}
	return nil
}

// Status 读取被控端系统状态
func (c *RemoteClient) Status() (map[string]any, error) {
	var out map[string]any
	err := c.getJSON("/panel/api/server/status", &out)
	return out, err
}

// Inbounds 读取被控端入站列表
func (c *RemoteClient) Inbounds() ([]map[string]any, error) {
	var out []map[string]any
	err := c.getJSON("/panel/api/inbounds/list", &out)
	return out, err
}

// SetInboundEnable 启停被控端指定入站
func (c *RemoteClient) SetInboundEnable(id int, enable bool) error {
	var inbound map[string]any
	if err := c.getJSON(fmt.Sprintf("/panel/api/inbounds/get/%d", id), &inbound); err != nil {
		return err
	}
	inbound["enable"] = enable
	payload, _ := json.Marshal(inbound)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/panel/api/inbounds/update/%d", c.base, id), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r remoteResp
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("被控端返回异常(HTTP %d)", resp.StatusCode)
	}
	if !r.Success {
		return fmt.Errorf("%s", r.Msg)
	}
	return nil
}
