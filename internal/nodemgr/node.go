package nodemgr

// 此处需要 atomic 辅助，避免 import cycle
// 临时用简单 getter

func atomicInt32(v int32) int32 { return v }
