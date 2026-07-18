package config

import (
	"log/slog"

	"github.com/fsnotify/fsnotify"
)

// NodeManager 节点管理器接口（避免循环依赖）
type NodeManager interface {
	ReloadNodes(cfg interface{})
}

// StartWatcher 启动配置文件热加载
func StartWatcher(path string, cfg *Config, mgr NodeManager) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, err
	}

	go func() {
		for event := range watcher.Events {
			if event.Op&fsnotify.Write != 0 || event.Op&fsnotify.Create != 0 {
				slog.Info("config file changed, reloading", "path", path)
				newCfg, err := Load(path)
				if err != nil {
					slog.Error("failed to reload config", "error", err)
					continue
				}
				// 原子替换配置
				*cfg = *newCfg
				if mgr != nil {
					mgr.ReloadNodes(cfg)
				}
				slog.Info("config reloaded successfully")
			}
		}
	}()

	return watcher, nil
}
