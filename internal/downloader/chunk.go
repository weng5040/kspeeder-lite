package downloader

// chunk.go — 分块管理（阶段二实现）
// 分块策略: chunkSize = clamp(totalSize / k, [chunkMinSize, chunkMaxSize])
