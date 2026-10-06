package evidence

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBodyPublishesVerifiedUnicodeContent(t *testing.T) {
	store := New(nil, nil, filepath.Join(t.TempDir(), "한국어 증거"))
	body := "한국어 요청과 응답\nChinese compatibility: 中文\n"
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	for i := 0; i < 2; i++ {
		hash, err := store.writeBody(strings.NewReader(body), int64(len(body)), expected)
		if err != nil || hash != expected {
			t.Fatalf("write %d: %q %v", i, hash, err)
		}
		path, err := hashPath(store.Dir, hash)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyFile(path, hash, int64(len(body))); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.Dir, ".staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging not cleaned: %v %v", entries, err)
	}
}

func TestWriteBodyRejectsBadContentWithoutPublishing(t *testing.T) {
	for _, tc := range []struct {
		name, hash string
		size       int64
	}{
		{"length", "", 100}, {"hash", strings.Repeat("0", 64), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, nil, t.TempDir())
			if _, err := s.writeBody(strings.NewReader("abc"), tc.size, tc.hash); err == nil {
				t.Fatal("bad body accepted")
			}
			if _, err := os.Stat(filepath.Join(s.Dir, "blobs")); !os.IsNotExist(err) {
				t.Fatalf("bad body published: %v", err)
			}
		})
	}
}

func TestInstallBodyPropagatesMissingSource(t *testing.T) {
	dir := t.TempDir()
	to := filepath.Join(dir, "destination")
	if err := installBody(filepath.Join(dir, "missing"), to); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatalf("destination created on failure: %v", err)
	}
}
