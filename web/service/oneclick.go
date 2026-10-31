package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"time"

	"x-ui/database/model"
)

var realityTargets = []string{
	"www.microsoft.com:443",
	"www.apple.com:443",
	"www.amazon.com:443",
	"www.cloudflare.com:443",
	"www.bing.com:443",
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "0123456789abcdef"
	}
	return hex.EncodeToString(b)
}

func pickFreePort() int {
	for i := 0; i < 30; i++ {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			continue
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if port >= 10000 && port <= 60000 {
			return port
		}
	}
	return 20000 + int(time.Now().UnixNano()%30000)
}

// CreateOneClickReality 生成一个可用的 VLESS + Reality 入站，并返回对应分享链接
func CreateOneClickReality(inboundService *InboundService, serverService *ServerService, remark string) (string, error) {
	if inboundService == nil {
		return "", fmt.Errorf("入站服务未初始化")
	}
	if remark == "" {
		remark = "oneclick-" + randomHex(4)
	}

	kpAny, err := serverService.GetNewX25519Cert()
	if err != nil {
		return "", fmt.Errorf("生成 Reality 密钥失败: %v", err)
	}
	kp, ok := kpAny.(map[string]any)
	if !ok {
		return "", fmt.Errorf("Reality 密钥格式异常")
	}
	privateKey, _ := kp["privateKey"].(string)
	publicKey, _ := kp["publicKey"].(string)
	if privateKey == "" || publicKey == "" {
		return "", fmt.Errorf("Reality 密钥为空")
	}

	uuidAny, err := serverService.GetNewUUID()
	if err != nil {
		return "", fmt.Errorf("生成 UUID 失败: %v", err)
	}
	clientId := uuidAny["uuid"]
	if clientId == "" {
		return "", fmt.Errorf("UUID 为空")
	}

	port := pickFreePort()
	target := realityTargets[time.Now().UnixNano()%int64(len(realityTargets))]
	sni := target
	for i, c := range target {
		if c == ':' {
			sni = target[:i]
			break
		}
	}
	shortId := randomHex(4)

	type realitySettings struct {
		Show        bool           `json:"show"`
		Target      string         `json:"target"`
		Xver        int            `json:"xver"`
		ServerNames []string       `json:"serverNames"`
		Settings    map[string]any `json:"settings"`
		PrivateKey  string         `json:"privateKey"`
		ShortIds    []string       `json:"shortIds"`
		Mldsa65Seed string         `json:"mldsa65Seed"`
	}
	stream := map[string]any{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": realitySettings{
			Show:        false,
			Target:      target,
			Xver:        0,
			ServerNames: []string{sni, "www." + sni},
			Settings: map[string]any{
				"publicKey":     publicKey,
				"spiderX":       "/",
				"mldsa65Verify": "",
			},
			PrivateKey:  privateKey,
			ShortIds:    []string{shortId},
			Mldsa65Seed: "",
		},
		"tcpSettings": map[string]any{
			"acceptProxyProtocol": false,
			"header":              map[string]any{"type": "none"},
		},
	}
	streamBytes, _ := json.Marshal(stream)

	settings := map[string]any{
		"clients": []map[string]any{{
			"id":      clientId,
			"flow":    "xtls-rprx-vision",
			"email":   remark,
			"level":   0,
			"enable":  true,
			"limitIp": 0,
			"totalGB": 0,
		}},
		"decryption": "none",
		"fallbacks":  []any{},
	}
	settingsBytes, _ := json.Marshal(settings)
	sniffingBytes, _ := json.Marshal(map[string]any{
		"enabled":      true,
		"destOverride": []string{"http", "tls", "quic", "fakedns"},
		"metadataOnly": false,
		"routeOnly":    false,
	})

	inbound := &model.Inbound{
		UserId:         1,
		Up:             0,
		Down:           0,
		Total:          0,
		Remark:         remark,
		Enable:         true,
		ExpiryTime:     0,
		Listen:         "",
		Port:           port,
		Protocol:       model.VLESS,
		Settings:       string(settingsBytes),
		StreamSettings: string(streamBytes),
		Tag:            "oneclick-" + remark,
		Sniffing:       string(sniffingBytes),
	}
	if _, _, err := inboundService.AddInbound(inbound); err != nil {
		return "", fmt.Errorf("创建入站失败: %v", err)
	}
	if serverService != nil {
		serverService.OpenPort(fmt.Sprintf("%d", port))
	}

	host := getPublicIP("https://api.ipify.org")
	if host == "N/A" || host == "" {
		host = getPublicIP("https://ipv4.icanhazip.com")
	}
	if host == "N/A" || host == "" {
		host = "127.0.0.1"
	}

	q := url.Values{}
	q.Set("type", "tcp")
	q.Set("encryption", "none")
	q.Set("security", "reality")
	q.Set("pbk", publicKey)
	q.Set("fp", "chrome")
	q.Set("sni", sni)
	q.Set("sid", shortId)
	q.Set("spx", "/")
	q.Set("flow", "xtls-rprx-vision")

	link := fmt.Sprintf("vless://%s@%s:%d?%s#%s", clientId, host, port, q.Encode(), url.QueryEscape(remark))
	return link, nil
}
