package cache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cache 基于 digest 的本地磁盘 blob 缓存，使用 LRU 淘汰策略。
type Cache struct {
	dir         string
	maxSize     int64
	usedSize    atomic.Int64
	count       atomic.Int64
	mu          sync.Mutex
	accessTimes map[string]time.Time // file basename -> last access time
}

// NewCache 创建缓存实例，自动创建缓存目录。
func NewCache(dir string, maxSize int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	c := &Cache{
		dir:         dir,
		maxSize:     maxSize,
		accessTimes: make(map[string]time.Time),
	}
	c.scanExisting()
	return c, nil
}

// scanExisting 扫描现有缓存文件，恢复使用量和计数。
func (c *Cache) scanExisting() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".blob") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c.usedSize.Add(info.Size())
		c.count.Add(1)
		c.accessTimes[e.Name()] = info.ModTime()
	}
}

// digestToPath 将 digest 转换为缓存文件路径。
// "sha256:abc123..." -> "{dir}/sha256-abc123....blob"
func digestToPath(dir, digest string) string {
	hex := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(dir, fmt.Sprintf("sha256-%s.blob", hex))
}

// Get 从缓存中获取 blob，返回 reader、文件大小和是否命中。
// 调用者负责关闭返回的 reader。
func (c *Cache) Get(digest string) (io.ReadCloser, int64, bool) {
	path := digestToPath(c.dir, digest)
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false
	}

	// 更新访问时间（LRU 排序依据）
	c.mu.Lock()
	c.accessTimes[filepath.Base(path)] = time.Now()
	c.mu.Unlock()

	return f, info.Size(), true
}

// Put 将 blob 写入缓存。若空间不足，自动触发 LRU 淘汰。
func (c *Cache) Put(digest string, reader io.Reader, size int64) error {
	path := digestToPath(c.dir, digest)

	c.mu.Lock()
	defer c.mu.Unlock()

	// 文件已存在，跳过
	if _, err := os.Stat(path); err == nil {
		c.accessTimes[filepath.Base(path)] = time.Now()
		return nil
	}

	// 检查并淘汰
	used := c.usedSize.Load()
	if used+size > c.maxSize {
		needed := used + size - c.maxSize
		if err := c.evictLocked(needed); err != nil {
			return fmt.Errorf("evict: %w", err)
		}
		if c.usedSize.Load()+size > c.maxSize {
			return fmt.Errorf("insufficient space after eviction: have %d, need %d more", c.maxSize-c.usedSize.Load(), size)
		}
	}

	// 写入临时文件，再原子 rename
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	written, err := io.Copy(f, reader)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write cache file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename cache file: %w", err)
	}

	c.usedSize.Add(written)
	c.count.Add(1)
	c.accessTimes[filepath.Base(path)] = time.Now()
	return nil
}

// Remove 从缓存中删除指定 blob。
func (c *Cache) Remove(digest string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := digestToPath(c.dir, digest)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	c.usedSize.Add(-info.Size())
	c.count.Add(-1)
	delete(c.accessTimes, filepath.Base(path))
	return nil
}

// Stats 返回缓存统计信息：当前使用量、最大容量、文件数量。
func (c *Cache) Stats() (used int64, max int64, count int) {
	return c.usedSize.Load(), c.maxSize, int(c.count.Load())
}

// evictLocked 淘汰最旧的缓存文件以释放空间。调用前需持有 c.mu 锁。
func (c *Cache) evictLocked(needed int64) error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}

	type fileEntry struct {
		path string
		size int64
		ts   time.Time
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".blob") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		ts, ok := c.accessTimes[e.Name()]
		if !ok {
			ts = info.ModTime()
		}
		files = append(files, fileEntry{
			path: filepath.Join(c.dir, e.Name()),
			size: info.Size(),
			ts:   ts,
		})
	}

	if len(files) == 0 {
		return fmt.Errorf("no cache files to evict")
	}

	// 按访问时间排序，最旧的优先淘汰
	sort.Slice(files, func(i, j int) bool {
		return files[i].ts.Before(files[j].ts)
	})

	var freed int64
	for _, f := range files {
		if freed >= needed {
			break
		}
		if err := os.Remove(f.path); err != nil {
			continue
		}
		freed += f.size
		c.usedSize.Add(-f.size)
		c.count.Add(-1)
		delete(c.accessTimes, filepath.Base(f.path))
	}

	return nil
}
