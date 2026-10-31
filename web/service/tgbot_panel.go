package service

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

var panelMu sync.RWMutex

// activePanels: chatId -> 被控端 id，0 表示本机
var activePanels = map[int64]int{}

func getActivePanel(chatId int64) int {
	panelMu.RLock()
	defer panelMu.RUnlock()
	return activePanels[chatId]
}

func setActivePanel(chatId int64, id int) {
	panelMu.Lock()
	defer panelMu.Unlock()
	activePanels[chatId] = id
}

func fmtBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(v)/float64(div), "KMGTPE"[exp])
}

// SwitchPanelMenu 展示可切换的面板列表（本机 + 已添加的被控端）
func (t *Tgbot) SwitchPanelMenu(chatId int64) {
	svc := RemoteServerService{}
	servers, err := svc.GetAll()
	if err != nil {
		t.SendMsgToTgbot(chatId, "读取被控端列表失败："+err.Error())
		return
	}

	rows := [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🖥 本机（默认）").WithCallbackData(t.encodeQuery("panel_switch:0")),
		),
	}
	for _, s := range servers {
		label := s.Name
		if s.Type == 1 {
			label = "🔁 " + label
		}
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(t.encodeQuery(fmt.Sprintf("panel_switch:%d", s.Id))),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("❌ 关闭").WithCallbackData(t.encodeQuery("panel_close")),
	))

	current := getActivePanel(chatId)
	hint := "当前控制：本机"
	if current != 0 {
		for _, s := range servers {
			if s.Id == current {
				hint = "当前控制：" + s.Name
			}
		}
	}
	t.SendMsgToTgbot(chatId, "🖥 <b>切换控制 VPS</b>\n\n"+hint+"\n\n请选择要控制的面板：", tu.InlineKeyboard(rows...))
}

// SwitchPanel 切换当前聊天窗控制的面板
func (t *Tgbot) SwitchPanel(chatId int64, id int) {
	if id == 0 {
		setActivePanel(chatId, 0)
		t.PanelSummary(chatId)
		return
	}
	svc := RemoteServerService{}
	server, err := svc.Get(id)
	if err != nil {
		t.SendMsgToTgbot(chatId, "❌ 被控端不存在，请先在面板【主从管理】里添加")
		return
	}
	client, err := svc.ClientFor(server)
	if err != nil {
		t.SendMsgToTgbot(chatId, fmt.Sprintf("❌ 连接被控端〔%s〕失败：\n%s", server.Name, err.Error()))
		return
	}
	if _, err := client.Inbounds(); err != nil {
		t.SendMsgToTgbot(chatId, fmt.Sprintf("❌ 被控端〔%s〕接口异常：\n%s", server.Name, err.Error()))
		return
	}
	setActivePanel(chatId, id)
	t.PanelSummary(chatId)
}

// PanelSummary 输出当前控制面板的状态概览
func (t *Tgbot) PanelSummary(chatId int64) {
	id := getActivePanel(chatId)
	var text string
	var name string

	if id == 0 {
		name = "本机"
		status := t.serverService.GetStatus(t.lastStatus)
		count := 0
		if t.inboundService != nil {
			if list, err := t.inboundService.GetAllInbounds(); err == nil {
				count = len(list)
			}
		}
		text = fmt.Sprintf("🖥 <b>当前控制：%s</b>\n\nCPU：%.1f%%\n内存：%s / %s\n入站：%d 个\nXray：%s",
			name, status.Cpu, fmtBytes(status.Mem.Current), fmtBytes(status.Mem.Total), count, status.Xray.State)
	} else {
		svc := RemoteServerService{}
		server, err := svc.Get(id)
		if err != nil {
			setActivePanel(chatId, 0)
			t.SendMsgToTgbot(chatId, "被控端已不存在，已自动切回本机")
			return
		}
		name = server.Name
		client, err := svc.ClientFor(server)
		if err != nil {
			t.SendMsgToTgbot(chatId, fmt.Sprintf("🖥 当前控制：%s\n\n❌ 连接失败：%s", name, err.Error()))
			return
		}
		st, err := client.Status()
		if err != nil {
			t.SendMsgToTgbot(chatId, fmt.Sprintf("🖥 当前控制：%s\n\n❌ 状态读取失败：%s", name, err.Error()))
			return
		}
		inbounds, _ := client.Inbounds()
		cpu, _ := st["cpu"].(float64)
		mem, _ := st["mem"].(map[string]any)
		var memCur, memTotal uint64
		if mem != nil {
			if v, ok := mem["current"].(float64); ok {
				memCur = uint64(v)
			}
			if v, ok := mem["total"].(float64); ok {
				memTotal = uint64(v)
			}
		}
		xrayState := "unknown"
		if x, ok := st["xray"].(map[string]any); ok {
			if s, ok := x["state"].(string); ok {
				xrayState = s
			}
		}
		text = fmt.Sprintf("🖥 <b>当前控制：%s</b>\n地址：%s\n\nCPU：%.1f%%\n内存：%s / %s\n入站：%d 个\nXray：%s",
			name, server.Url, cpu, fmtBytes(memCur), fmtBytes(memTotal), len(inbounds), xrayState)
	}

	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔄 刷新").WithCallbackData(t.encodeQuery("panel_refresh")),
			tu.InlineKeyboardButton("🖥 切换 VPS").WithCallbackData(t.encodeQuery("panel_menu")),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ 关闭").WithCallbackData(t.encodeQuery("panel_close")),
		),
	)
	t.SendMsgToTgbot(chatId, strings.TrimSpace(text), kb)
}

// remoteInboundsText 当前控制的是被控端时，返回其入站列表文本
func (t *Tgbot) remoteInboundsText(serverId int) (string, bool) {
	svc := RemoteServerService{}
	server, err := svc.Get(serverId)
	if err != nil {
		return "", false
	}
	client, err := svc.ClientFor(server)
	if err != nil {
		return fmt.Sprintf("❌ 连接〔%s〕失败：%s", server.Name, err.Error()), true
	}
	list, err := client.Inbounds()
	if err != nil {
		return fmt.Sprintf("❌ 读取〔%s〕入站失败：%s", server.Name, err.Error()), true
	}
	if len(list) == 0 {
		return fmt.Sprintf("🖥 %s\n\n当前没有任何入站。", server.Name), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🖥 %s 的入站（%d 个）\n\n", server.Name, len(list))
	for _, ib := range list {
		state := "🟢"
		if en, ok := ib["enable"].(bool); ok && !en {
			state = "⚪️"
		}
		fmt.Fprintf(&b, "%s #%v %v [%v:%v] ⬆%v ⬇%v\n",
			state, ib["id"], ib["remark"], ib["protocol"], ib["port"], ib["up"], ib["down"])
	}
	b.WriteString("\n提示：在面板后台可对被控端入站做增删改。")
	return b.String(), true
}
