package cache

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCache(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024) // 1MB
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}
	used, max, count := c.Stats()
	if max != 1024*1024 {
		t.Errorf("expected maxSize=1048576, got %d", max)
	}
	if used != 0 {
		t.Errorf("expected used=0, got %d", used)
	}
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	digest := "sha256:abc123def456"
	data := []byte("hello cache world")
	reader := bytes.NewReader(data)

	if err := c.Put(digest, reader, int64(len(data))); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 验证文件存在
	path := digestToPath(dir, digest)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file not found: %v", err)
	}

	// Get 命中
	r, size, ok := c.Get(digest)
	if !ok {
		t.Fatal("Get should hit cache")
	}
	defer r.Close()

	if size != int64(len(data)) {
		t.Errorf("expected size=%d, got %d", len(data), size)
	}

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: got %q, want %q", got, data)
	}

	// Stats
	used, max, count := c.Stats()
	if used != int64(len(data)) {
		t.Errorf("expected used=%d, got %d", len(data), used)
	}
	if max != 1024*1024 {
		t.Errorf("expected max=1048576, got %d", max)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
}

func TestGetMiss(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	_, _, ok := c.Get("sha256:nonexistent")
	if ok {
		t.Error("Get should miss for nonexistent digest")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	digest := "sha256:test-remove"
	data := []byte("remove me")
	if err := c.Put(digest, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := c.Remove(digest); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	_, _, ok := c.Get(digest)
	if ok {
		t.Error("Get should miss after Remove")
	}

	used, _, count := c.Stats()
	if used != 0 {
		t.Errorf("expected used=0 after remove, got %d", used)
	}
	if count != 0 {
		t.Errorf("expected count=0 after remove, got %d", count)
	}
}

func TestRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// Remove 不存在的文件不应报错
	if err := c.Remove("sha256:nonexistent"); err != nil {
		t.Fatalf("Remove nonexistent should not error: %v", err)
	}
}

func TestEvictOnFull(t *testing.T) {
	dir := t.TempDir()
	// 只允许存 100 字节
	c, err := NewCache(dir, 100)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// 写入 60 字节
	data1 := bytes.Repeat([]byte("a"), 60)
	if err := c.Put("sha256:first", bytes.NewReader(data1), int64(len(data1))); err != nil {
		t.Fatalf("Put first: %v", err)
	}

	// 写入 50 字节 — 超出 100，应淘汰最旧的 first
	data2 := bytes.Repeat([]byte("b"), 50)
	if err := c.Put("sha256:second", bytes.NewReader(data2), int64(len(data2))); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	// first 应被淘汰
	_, _, ok := c.Get("sha256:first")
	if ok {
		t.Error("first should be evicted")
	}

	// second 应在
	_, _, ok = c.Get("sha256:second")
	if !ok {
		t.Error("second should exist")
	}

	used, _, count := c.Stats()
	if used != 50 {
		t.Errorf("expected used=50, got %d", used)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
}

func TestPutIdempotent(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	digest := "sha256:idempotent"
	data := []byte("same data")

	if err := c.Put(digest, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	// 再次 Put 同名，应无错误
	if err := c.Put(digest, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	used, _, count := c.Stats()
	if used != int64(len(data)) {
		t.Errorf("expected used=%d, got %d", len(data), used)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
}

func TestScanExisting(t *testing.T) {
	dir := t.TempDir()

	// 事先在目录中放一个 .blob 文件
	existingPath := filepath.Join(dir, "sha256-preexisting.blob")
	if err := os.WriteFile(existingPath, []byte("pre-existing data"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	// 也放一个非 .blob 文件，应该被忽略
	otherPath := filepath.Join(dir, "not-a-blob.txt")
	if err := os.WriteFile(otherPath, []byte("ignore me"), 0644); err != nil {
		t.Fatalf("write other file: %v", err)
	}

	// 创建缓存，应扫描到已有文件
	c, err := NewCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	used, _, count := c.Stats()
	if used != 17 { // "pre-existing data" = 17 bytes
		t.Errorf("expected used=17, got %d", used)
	}
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// 已有文件应可 Get
	r, size, ok := c.Get("sha256:preexisting")
	if !ok {
		t.Fatal("should find pre-existing file")
	}
	defer r.Close()
	if size != 17 {
		t.Errorf("expected size=17, got %d", size)
	}
}

func TestMultipleEvictions(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 100)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// 写入 3 个文件，分别 40、40、40 字节
	for i, label := range []string{"a", "b", "c"} {
		data := bytes.Repeat([]byte(label), 40)
		digest := "sha256:" + label
		if err := c.Put(digest, bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatalf("Put %s: %v", label, err)
		}
		_ = i
	}

	_, _, count := c.Stats()
	if count != 2 {
		t.Errorf("expected count=2 after eviction, got %d", count)
	}
}

func TestDigestToPath(t *testing.T) {
	path := digestToPath("/cache", "sha256:abc123")
	expected := filepath.Join("/cache", "sha256-abc123.blob")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}
