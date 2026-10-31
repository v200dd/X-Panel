package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"x-ui/config"
	"x-ui/database"
)

// SnapshotService 数据快照 / 应急恢复：把当前数据库做成本地快照文件
type SnapshotService struct{}

var snapshotMutex sync.Mutex

func (s *SnapshotService) snapshotDir() string {
	return filepath.Join(config.GetDBFolderPath(), "snapshots")
}

func (s *SnapshotService) ensureDir() (string, error) {
	dir := s.snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func validSnapshotName(name string) bool {
	if !strings.HasPrefix(name, "snapshot-") || !strings.HasSuffix(name, ".db") {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

func (s *SnapshotService) path(name string) (string, error) {
	if !validSnapshotName(name) {
		return "", fmt.Errorf("非法的快照文件名")
	}
	return filepath.Join(s.snapshotDir(), name), nil
}

// Create 使用 SQLite 的 VACUUM INTO 生成一致性快照（安全，不影响正在运行的面板）
func (s *SnapshotService) Create() (map[string]any, error) {
	snapshotMutex.Lock()
	defer snapshotMutex.Unlock()

	dir, err := s.ensureDir()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("snapshot-%s.db", time.Now().Format("20060102-150405"))
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		return nil, fmt.Errorf("同名快照已存在，请稍后重试")
	}
	if err := database.GetDB().Exec("VACUUM INTO ?", target).Error; err != nil {
		return nil, fmt.Errorf("创建快照失败: %v", err)
	}
	info, _ := os.Stat(target)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return map[string]any{"name": name, "size": size, "time": time.Now().Unix()}, nil
}

func (s *SnapshotService) List() ([]map[string]any, error) {
	dir, err := s.ensureDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !validSnapshotName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, map[string]any{
			"name": e.Name(),
			"size": info.Size(),
			"time": info.ModTime().Unix(),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i]["time"].(int64) > list[j]["time"].(int64)
	})
	return list, nil
}

func (s *SnapshotService) Delete(name string) error {
	snapshotMutex.Lock()
	defer snapshotMutex.Unlock()
	p, err := s.path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *SnapshotService) File(name string) (string, error) {
	p, err := s.path(name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("快照不存在")
	}
	return p, nil
}

// Restore 用快照覆盖当前数据库，然后重新初始化数据库连接
func (s *SnapshotService) Restore(name string) error {
	snapshotMutex.Lock()
	defer snapshotMutex.Unlock()

	src, err := s.path(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("快照不存在")
	}
	dbPath := config.GetDBPath()
	// 先把当前库另存一份，避免恢复失败后无法回退
	if err := copyFile(dbPath, dbPath+".before-restore"); err != nil {
		return fmt.Errorf("备份当前数据库失败: %v", err)
	}
	if err := database.CloseDB(); err != nil {
		return err
	}
	if err := copyFile(src, dbPath); err != nil {
		// 回滚
		_ = copyFile(dbPath+".before-restore", dbPath)
		_ = database.InitDB(dbPath)
		return fmt.Errorf("覆盖数据库失败: %v", err)
	}
	if err := database.InitDB(dbPath); err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}
