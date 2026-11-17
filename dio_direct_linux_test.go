//go:build linux

package fastBuffer

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func fillDioPattern(buf []byte) {
	for i := range buf {
		buf[i] = byte(i%251 + 1)
	}
}

func readDioFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_DIRECT, 0644)
	if err != nil {
		t.Fatalf("open O_DIRECT for read: %v", err)
	}
	defer f.Close()

	fb, err := NewDioFB(size, 4096)
	if err != nil {
		t.Fatalf("NewDioFB for read: %v", err)
	}
	defer FreeFB(fb)
	buf := fb.GetBuf()[:size]
	n, err := f.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("O_DIRECT read: %v", err)
	}
	if n != size {
		t.Fatalf("O_DIRECT read length = %d, want %d", n, size)
	}
	return append([]byte(nil), buf...)
}

func TestDioFBO_DIRECTLinux(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dio.bin")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_DIRECT, 0644)
	if err != nil {
		t.Fatalf("open O_DIRECT for write: %v", err)
	}
	defer f.Close()

	const size = 1 << 20
	fb, err := NewDioFB(size, 4096)
	if err != nil {
		t.Fatalf("NewDioFB: %v", err)
	}
	defer FreeFB(fb)

	fillDioPattern(fb.GetBuf()[:size])
	if n, err := f.WriteAt(fb.GetBuf()[:size], 0); err != nil || n != size {
		t.Fatalf("O_DIRECT write = %d, %v", n, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync O_DIRECT file: %v", err)
	}

	got := readDioFile(t, path, size)
	if want := fb.GetBuf()[:size]; !bytes.Equal(got, want) {
		t.Fatal("O_DIRECT read/write data mismatch")
	}
}

func TestWrappedDioO_DIRECTLinux(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapped-dio.bin")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_DIRECT, 0644)
	if err != nil {
		t.Fatalf("open O_DIRECT for write: %v", err)
	}
	defer f.Close()

	const size = 64 << 10
	raw := Allocate(size + 4096)
	defer Free(raw)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	data := raw[off : off+size : off+size]

	fb, err := WrapDio(data, 4096)
	if err != nil {
		t.Fatalf("WrapDio: %v", err)
	}
	defer FreeFB(fb)

	fillDioPattern(fb.GetBuf())
	if n, err := f.WriteAt(fb.GetBuf(), 0); err != nil || n != size {
		t.Fatalf("O_DIRECT wrapped write = %d, %v", n, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync wrapped O_DIRECT file: %v", err)
	}

	got := readDioFile(t, path, size)
	if !bytes.Equal(got, fb.GetBuf()) {
		t.Fatal("O_DIRECT wrapped read/write data mismatch")
	}
}
