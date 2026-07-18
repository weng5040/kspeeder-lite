package nodemgr

import (
	"sync"
	"time"
)

// NodeType 节点类型
type NodeType string

const (
	NodeTypeMirror NodeType = "mirror"
	NodeTypeSocks5 NodeType = "socks5"
	NodeTypeHTTP   NodeType = "http"
)

// Node 节点定义
type Node struct {
	URL         string
	DisplayName string
	Type        NodeType
	Priority    int
	Enabled     bool
	Targets     []string

	Speed     float64
	FailCount int
	InFlight  int32 // atomic
	LastCheck time.Time
	Healthy   bool

	mu sync.Mutex
}

// atomicInt32 简单 getter，避免直接访问 atomic 字段
func atomicInt32(v int32) int32 { return v }
