package factorio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMimallocPath_V3Preferred(t *testing.T) {
	tmp := t.TempDir()
	v3 := filepath.Join(tmp, "libmimalloc.so.3")
	v2 := filepath.Join(tmp, "libmimalloc.so.2")
	unver := filepath.Join(tmp, "libmimalloc.so")

	for _, p := range []string{v3, v2, unver} {
		if err := os.WriteFile(p, []byte("fake"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", p, err)
		}
	}

	got := resolveMimallocPath("", []string{v3, v2, unver})
	if got != v3 {
		t.Fatalf("expected v3 %q, got %q", v3, got)
	}
}

func TestResolveMimallocPath_FallbackToV2(t *testing.T) {
	tmp := t.TempDir()
	v2 := filepath.Join(tmp, "libmimalloc.so.2")
	if err := os.WriteFile(v2, []byte("fake"), 0644); err != nil {
		t.Fatalf("failed to create %s: %v", v2, err)
	}

	got := resolveMimallocPath("", []string{
		filepath.Join(tmp, "libmimalloc.so.3"),
		v2,
	})
	if got != v2 {
		t.Fatalf("expected v2 fallback %q, got %q", v2, got)
	}
}

func TestResolveMimallocPath_FallbackToUnversioned(t *testing.T) {
	tmp := t.TempDir()
	unver := filepath.Join(tmp, "libmimalloc.so")
	if err := os.WriteFile(unver, []byte("fake"), 0644); err != nil {
		t.Fatalf("failed to create %s: %v", unver, err)
	}

	got := resolveMimallocPath("", []string{
		filepath.Join(tmp, "libmimalloc.so.3"),
		filepath.Join(tmp, "libmimalloc.so.2"),
		unver,
	})
	if got != unver {
		t.Fatalf("expected unversioned fallback %q, got %q", unver, got)
	}
}

func TestResolveMimallocPath_NoneFound(t *testing.T) {
	tmp := t.TempDir()
	got := resolveMimallocPath("", []string{
		filepath.Join(tmp, "libmimalloc.so.3"),
		filepath.Join(tmp, "libmimalloc.so.2"),
	})
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestResolveMimallocPath_OverrideWins(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-mimalloc.so")
	got := resolveMimallocPath(override, []string{
		"/usr/lib/x86_64-linux-gnu/libmimalloc.so.3",
	})
	if got != override {
		t.Fatalf("expected override %q, got %q", override, got)
	}
}

func TestDefaultMimallocSearchPaths(t *testing.T) {
	expected := []string{
		"/usr/lib/x86_64-linux-gnu/libmimalloc.so.3",
		"/usr/lib/x86_64-linux-gnu/libmimalloc.so.2",
		"/usr/lib/x86_64-linux-gnu/libmimalloc.so",
		"/usr/lib/libmimalloc.so",
	}
	got := defaultMimallocSearchPaths()
	if len(got) != len(expected) {
		t.Fatalf("expected %d paths, got %d", len(expected), len(got))
	}
	for i, p := range expected {
		if got[i] != p {
			t.Errorf("path %d: expected %q, got %q", i, p, got[i])
		}
	}
}
