package ve_tos

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
)

var bucket = "veadk-ut-20251208152204"

func getClientOrSkip(t *testing.T) Client {
	t.Helper()
	ak := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY)
	sk := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY)
	t.Log("ak=", ak)
	t.Log("sk=", sk)
	if ak == "" || sk == "" {
		t.Skip("missing required env: VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY")
	}
	client, err := New(&Config{AK: ak, SK: sk, Region: "cn-beijing", Bucket: bucket})
	if err != nil {
		t.Fatal(err)
		t.Skip("missing required env: VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY")
	}
	return *client
}

func TestNew_DefaultEndpoint(t *testing.T) {
	t.Parallel()
	cfg := &Config{AK: "ak", SK: "sk", Region: "cn-beijing"}
	c, err := New(cfg)
	if err != nil {
		t.Errorf("New client got error: %v", err)
	}
	if c == nil {
		t.Fatalf("expected client, got nil")
	}
	expected := "https://tos-" + cfg.Region + ".volces.com"
	if c.config.Endpoint != expected {
		t.Fatalf("endpoint mismatch: want %s, got %s", expected, c.config.Endpoint)
	}
	if c.client == nil {
		t.Fatalf("expected underlying TOS client, got nil")
	}
}

func uniqueKey(suffix string) string {
	return "ut-" + time.Now().Format("150405.000000") + "-" + suffix
}

func TestBucketExist_NotFound(t *testing.T) {
	c := getClientOrSkip(t)
	exist, err := c.BucketExist(t.Context())
	if err != nil {
		t.Fatalf("BucketExist returned error: %v", err)
	}
	if exist {
		t.Fatalf("expected bucket not exist, got exist: %s", bucket)
	}
}

func TestCreateAndDeleteBucket(t *testing.T) {
	c := getClientOrSkip(t)
	t.Log("creating bucket:", bucket)

	if err := c.CreateBucket(t.Context()); err != nil {
		t.Fatalf("CreateBucket error: %v", err)
	}
	// ensure cleanup
	defer func() { _ = c.DeleteBucket(t.Context()) }()

	exist, err := c.BucketExist(t.Context())
	if err != nil {
		t.Fatalf("BucketExist after create returned error: %v", err)
	}
	if !exist {
		t.Fatalf("expected bucket exist after create, got not exist: %s", bucket)
	}

	if err := c.DeleteBucket(t.Context()); err != nil {
		t.Fatalf("DeleteBucket error: %v", err)
	}

	// eventual consistency guard
	for i := 0; i < 5; i++ {
		exist, err = c.BucketExist(t.Context())
		if err != nil {
			t.Fatalf("BucketExist after delete returned error: %v", err)
		}
		if !exist {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("expected bucket not exist after delete, still exists: %s", bucket)
}

func TestUploadTextAndDownload(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("text.txt")
	text := "hello world"
	if err := c.UploadText(text, key, nil); err != nil {
		t.Fatalf("UploadText error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.txt"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != text {
		t.Fatalf("content mismatch: want %q, got %q", text, string(b))
	}
}

func TestAsyncUploadText(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("async-text.txt")
	ch := c.AsyncUploadText("abc", key, nil)
	if err := <-ch; err != nil {
		t.Fatalf("AsyncUploadText error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.txt"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != "abc" {
		t.Fatalf("content mismatch: want %q, got %q", "abc", string(b))
	}
}

func TestUploadBytesAndDownload(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("bytes.bin")
	data := []byte{0x61, 0x62, 0x63}
	if err := c.UploadBytes(data, key, nil); err != nil {
		t.Fatalf("UploadBytes error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.bin"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != string(data) {
		t.Fatalf("content mismatch: want %q, got %q", string(data), string(b))
	}
}

func TestAsyncUploadBytes(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("async-bytes.bin")
	data := []byte("xyz")
	ch := c.AsyncUploadBytes(data, key, nil)
	if err := <-ch; err != nil {
		t.Fatalf("AsyncUploadBytes error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.bin"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != string(data) {
		t.Fatalf("content mismatch: want %q, got %q", string(data), string(b))
	}
}

func TestUploadFileAndDownload(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("file.txt")
	src := filepath.Join(os.TempDir(), uniqueKey("src.txt"))
	if err := os.WriteFile(src, []byte("file-content"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	if err := c.UploadFile(src, key, nil); err != nil {
		t.Fatalf("UploadFile error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.txt"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != "file-content" {
		t.Fatalf("content mismatch: want %q, got %q", "file-content", string(b))
	}
}

func TestAsyncUploadFile(t *testing.T) {
	c := getClientOrSkip(t)

	key := uniqueKey("async-file.txt")
	src := filepath.Join(os.TempDir(), uniqueKey("src.txt"))
	if err := os.WriteFile(src, []byte("af"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	ch := c.AsyncUploadFile(src, key, nil)
	if err := <-ch; err != nil {
		t.Fatalf("AsyncUploadFile error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dl.txt"))
	if err := c.Download(key, dst); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(b) != "af" {
		t.Fatalf("content mismatch: want %q, got %q", "af", string(b))
	}
}

func TestUploadFilesAndDownload(t *testing.T) {
	c := getClientOrSkip(t)

	src1 := filepath.Join(os.TempDir(), uniqueKey("src1.txt"))
	src2 := filepath.Join(os.TempDir(), uniqueKey("src2.txt"))
	if err := os.WriteFile(src1, []byte("A"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	if err := os.WriteFile(src2, []byte("B"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	k1 := uniqueKey("k1.txt")
	k2 := uniqueKey("k2.txt")
	if err := c.UploadFiles([]string{src1, src2}, []string{k1, k2}, nil); err != nil {
		t.Fatalf("UploadFiles error: %v", err)
	}
	d1 := filepath.Join(os.TempDir(), uniqueKey("d1.txt"))
	d2 := filepath.Join(os.TempDir(), uniqueKey("d2.txt"))
	if err := c.Download(k1, d1); err != nil {
		t.Fatalf("Download k1 error: %v", err)
	}
	if err := c.Download(k2, d2); err != nil {
		t.Fatalf("Download k2 error: %v", err)
	}
	b1, _ := os.ReadFile(d1)
	b2, _ := os.ReadFile(d2)
	if string(b1) != "A" || string(b2) != "B" {
		t.Fatalf("content mismatch: got %q and %q", string(b1), string(b2))
	}
}

func TestAsyncUploadFiles(t *testing.T) {
	c := getClientOrSkip(t)

	src1 := filepath.Join(os.TempDir(), uniqueKey("src1.txt"))
	src2 := filepath.Join(os.TempDir(), uniqueKey("src2.txt"))
	t.Log("the files are ", src1, ",", src2)
	_ = os.WriteFile(src1, []byte("AA"), 0o644)
	_ = os.WriteFile(src2, []byte("BB"), 0o644)
	k1 := uniqueKey("ak1.txt")
	k2 := uniqueKey("ak2.txt")
	ch := c.AsyncUploadFiles([]string{src1, src2}, []string{k1, k2}, nil)
	if err := <-ch; err != nil {
		t.Fatalf("AsyncUploadFiles error: %v", err)
	}
	d1 := filepath.Join(os.TempDir(), uniqueKey("d1.txt"))
	d2 := filepath.Join(os.TempDir(), uniqueKey("d2.txt"))
	_ = c.Download(k1, d1)
	_ = c.Download(k2, d2)
	b1, _ := os.ReadFile(d1)
	b2, _ := os.ReadFile(d2)
	if string(b1) != "AA" || string(b2) != "BB" {
		t.Fatalf("content mismatch: got %q and %q", string(b1), string(b2))
	}
}

func TestUploadDirectoryAndDownload(t *testing.T) {
	c := getClientOrSkip(t)

	dir, err := os.MkdirTemp(os.TempDir(), "veadk-ut-")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	p := filepath.Join(dir, "nested")
	_ = os.MkdirAll(p, 0o755)
	p1f := filepath.Join(p, "a.txt")
	p2f := filepath.Join(p, "b.txt")
	_ = os.WriteFile(p1f, []byte("a"), 0o644)
	_ = os.WriteFile(p2f, []byte("b"), 0o644)
	if err := c.UploadDirectory(dir, nil); err != nil {
		t.Fatalf("UploadDirectory error: %v", err)
	}
	d1 := filepath.Join(os.TempDir(), uniqueKey("da.txt"))
	d2 := filepath.Join(os.TempDir(), uniqueKey("db.txt"))
	if err := c.Download("a.txt", d1); err != nil {
		t.Fatalf("Download a.txt error: %v", err)
	}
	if err := c.Download(filepath.ToSlash(filepath.Join("nested", "b.txt")), d2); err != nil {
		t.Fatalf("Download nested/b.txt error: %v", err)
	}
	b1, _ := os.ReadFile(d1)
	b2, _ := os.ReadFile(d2)
	if string(b1) != "a" || string(b2) != "b" {
		t.Fatalf("content mismatch: got %q and %q", string(b1), string(b2))
	}
}

func TestAsyncUploadDirectory(t *testing.T) {
	c := getClientOrSkip(t)

	dir, _ := os.MkdirTemp(os.TempDir(), "veadk-ut-")
	_ = os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644)
	ch := c.AsyncUploadDirectory(dir, nil)
	if err := <-ch; err != nil {
		t.Fatalf("AsyncUploadDirectory error: %v", err)
	}
	dst := filepath.Join(os.TempDir(), uniqueKey("dx.txt"))
	if err := c.Download("x.txt", dst); err != nil {
		t.Fatalf("Download x.txt error: %v", err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "x" {
		t.Fatalf("content mismatch: want %q, got %q", "x", string(b))
	}
}
