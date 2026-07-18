package registry

// Blob 下载逻辑（单节点透传）已集成到 api.go 的 GetBlob 中
// 多源并发下载将在阶段二接入 downloader 模块
