package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_New_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")

	c, err := New(cacheDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil Cache")
	}

	stat, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
	if !stat.IsDir() {
		t.Fatal("cache path is not a directory")
	}

	if (stat.Mode() & 0o700) != 0o700 {
		t.Errorf("cache dir permissions = %o, want 0o700", stat.Mode().Perm())
	}
}

func TestCache_New_ExistingDirectory(t *testing.T) {
	dir := t.TempDir()

	c1, err := New(dir)
	if err != nil {
		t.Fatalf("first New() failed: %v", err)
	}

	c2, err := New(dir)
	if err != nil {
		t.Fatalf("second New() failed: %v", err)
	}

	if c2 == nil {
		t.Fatal("expected non-nil Cache on second call")
	}
	if c1 == nil {
		t.Fatal("expected non-nil Cache on first call")
	}
}

func TestCache_SetGet_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	testData := map[string]interface{}{
		"name": "Alice",
		"age":  30,
		"tags": []string{"a", "b"},
	}

	c.Set("key1", testData, 10*time.Second)

	raw, ok := c.Get("key1")
	if !ok {
		t.Fatal("Get() returned false, expected true")
	}

	if raw == nil {
		t.Fatal("Get() returned nil data")
	}

	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got["name"] != testData["name"] {
		t.Errorf("name mismatch: %v != %v", got["name"], testData["name"])
	}
	if got["age"] != float64(testData["age"].(int)) {
		t.Errorf("age mismatch: %v != %v", got["age"], testData["age"])
	}
}

func TestCache_Get_UnsetKey(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	raw, ok := c.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent key, expected false")
	}
	if raw != nil {
		t.Error("Get() returned non-nil data for nonexistent key")
	}
}

func TestCache_Get_AfterExpiry(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	c.Set("expiring", map[string]string{"x": "y"}, 50*time.Millisecond)

	raw, ok := c.Get("expiring")
	if !ok {
		t.Fatal("Get() returned false before expiry")
	}
	if raw == nil {
		t.Fatal("Get() returned nil before expiry")
	}

	time.Sleep(100 * time.Millisecond)

	raw, ok = c.Get("expiring")
	if ok {
		t.Error("Get() returned true after expiry, expected false")
	}
	if raw != nil {
		t.Error("Get() returned non-nil after expiry")
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expired file not removed; entries in dir: %d", len(entries))
	}
}

func TestCache_Set_NegativeAndZeroTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{"zero", 0},
		{"negative", -5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			c, _ := New(dir)

			c.Set("key", map[string]string{"a": "b"}, tt.ttl)

			raw, ok := c.Get("key")
			if ok {
				t.Error("Get() returned true for zero/negative TTL, expected false")
			}
			if raw != nil {
				t.Error("Get() returned non-nil for zero/negative TTL")
			}
		})
	}
}

func TestCache_Set_UnmarshalableValue(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	ch := make(chan int)
	c.Set("channel", ch, 10*time.Second)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("Set() should silently drop unmarshallable value; found %d files in cache", len(entries))
	}

	raw, ok := c.Get("channel")
	if ok {
		t.Error("Get() returned true after Set with unmarshallable value")
	}
	if raw != nil {
		t.Error("Get() returned non-nil after Set with unmarshallable value")
	}
}

func TestCache_Set_SameKeyProducesOneFile(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	c.Set("key1", map[string]string{"x": "y"}, 10*time.Second)
	c.Set("key1", map[string]string{"a": "b"}, 10*time.Second)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("same key should produce one file; got %d", len(entries))
	}
}

func TestCache_Unmarshal_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	original := Person{Name: "Bob", Age: 25}
	c.Set("person", original, 10*time.Second)

	var retrieved Person
	ok := c.Unmarshal("person", &retrieved)
	if !ok {
		t.Fatal("Unmarshal() returned false, expected true")
	}

	if retrieved.Name != original.Name || retrieved.Age != original.Age {
		t.Errorf("unmarshal mismatch: %+v != %+v", retrieved, original)
	}
}

func TestCache_Unmarshal_Miss(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	var dst interface{}
	ok := c.Unmarshal("missing", &dst)
	if ok {
		t.Error("Unmarshal() returned true on miss, expected false")
	}
}

func TestCache_Unmarshal_AfterExpiry(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	type Data struct{ Value string }
	c.Set("data", Data{Value: "test"}, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	var dst Data
	ok := c.Unmarshal("data", &dst)
	if ok {
		t.Error("Unmarshal() returned true after expiry, expected false")
	}
}

func TestCache_Flush_RemovesAllFiles(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	for i := 0; i < 5; i++ {
		c.Set("key"+string(rune(i)), map[string]int{"n": i}, 10*time.Second)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 5 {
		t.Fatalf("expected 5 files after Set; got %d", len(entries))
	}

	count := c.Flush()
	if count != 5 {
		t.Errorf("Flush() returned %d, expected 5", count)
	}

	entries, _ = os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("Flush() should remove all files; %d remain", len(entries))
	}
}

func TestCache_Flush_EmptyCache(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	count := c.Flush()
	if count != 0 {
		t.Errorf("Flush() on empty cache returned %d, expected 0", count)
	}
}

func TestCache_Stats_LiveAndExpired(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	c.Set("live1", "data1", 10*time.Second)
	c.Set("live2", "data2", 10*time.Second)
	c.Set("expired1", "data3", 50*time.Millisecond)
	c.Set("expired2", "data4", 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	live, expired := c.Stats()
	if live != 2 {
		t.Errorf("Stats() returned live=%d, expected 2", live)
	}
	if expired != 2 {
		t.Errorf("Stats() returned expired=%d, expected 2", expired)
	}
}

func TestCache_Stats_AllLive(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	for i := 0; i < 3; i++ {
		c.Set("key"+string(rune(i)), "value", 10*time.Second)
	}

	live, expired := c.Stats()
	if live != 3 {
		t.Errorf("Stats() returned live=%d, expected 3", live)
	}
	if expired != 0 {
		t.Errorf("Stats() returned expired=%d, expected 0", expired)
	}
}

func TestCache_Stats_AllExpired(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	for i := 0; i < 3; i++ {
		c.Set("key"+string(rune(i)), "value", 50*time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	live, expired := c.Stats()
	if live != 0 {
		t.Errorf("Stats() returned live=%d, expected 0", live)
	}
	if expired != 3 {
		t.Errorf("Stats() returned expired=%d, expected 3", expired)
	}
}

func TestKey_CanonicalFormat(t *testing.T) {
	result := Key("gitlab.example.com", "/api/v4/issues", "p=1&q=2")
	expected := "gitlab.example.com|/api/v4/issues|p=1&q=2"
	if result != expected {
		t.Errorf("Key() = %q, expected %q", result, expected)
	}
}

func TestKey_EmptyParts(t *testing.T) {
	result := Key("", "", "")
	expected := "||"
	if result != expected {
		t.Errorf("Key() with empty parts = %q, expected %q", result, expected)
	}
}

func TestCache_DeterministicPaths(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)

	c.Set("key1", "value1", 10*time.Second)
	c.Set("key1", "value2", 10*time.Second)

	entries, _ := os.ReadDir(dir)
	fileNames := make([]string, 0)
	for _, e := range entries {
		fileNames = append(fileNames, e.Name())
	}

	if len(fileNames) != 1 {
		t.Errorf("same key should produce same file path; got %d files: %v", len(fileNames), fileNames)
	}
}
