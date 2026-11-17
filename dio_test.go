package fastBuffer

import (
	"bytes"
	"sync"
	"testing"
	"unsafe"
)

func baseAligned(buf []byte, align int) bool {
	return len(buf) > 0 && uintptr(unsafe.Pointer(&buf[0]))%uintptr(align) == 0
}

func TestDioAlignAndRound(t *testing.T) {
	cases := []struct{ size, align int }{
		{1, 512},
		{512, 512},
		{4096, 4096},
		{5000, 4096},
		{4 * MBSize, 4096},
		{100, 64 * 1024},
	}
	for _, c := range cases {
		fb, err := NewDioFB(c.size, c.align)
		if err != nil {
			t.Fatalf("NewDioFB(%d,%d): %v", c.size, c.align, err)
		}
		buf := fb.GetBuf()
		if !baseAligned(buf, c.align) {
			t.Fatalf("size %d align %d: base %p not aligned", c.size, c.align, &buf[0])
		}
		if len(buf) < c.size || len(buf)%c.align != 0 {
			t.Fatalf("size %d align %d: bad len %d", c.size, c.align, len(buf))
		}
		if fb.Align() != c.align {
			t.Fatalf("Align() = %d, want %d", fb.Align(), c.align)
		}
		FreeFB(fb)
	}
}

func TestDioInvalidArgs(t *testing.T) {
	for _, align := range []int{0, -1, 3, 4095} {
		if _, err := NewDioFB(4096, align); err == nil {
			t.Fatalf("align %d: want error", align)
		}
	}
	if _, err := NewDioFB(0, 4096); err == nil {
		t.Fatal("size 0: want error")
	}
	if _, err := NewDioFB(4096, 128*MBSize); err == nil {
		t.Fatal("align > max_size accepted")
	}
	maxInt := int(^uint(0) >> 1)
	if _, err := NewDioFB(maxInt-100, 4096); err == nil {
		t.Fatal("overflowing size accepted")
	}
}

func TestDioDedicatedPool4M(t *testing.T) {
	const size = 4 * MBSize
	const rawLen = size + 4096
	mem0, _, extra0 := Check()

	fb, err := NewDioFB(size, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if cap(fb.GetBuf()) != size {
		t.Fatalf("DIO view cap = %d, want %d", cap(fb.GetBuf()), size)
	}
	if _, ok := dioPools.Load(rawLen); !ok {
		t.Fatalf("missing exact-size DIO pool %d", rawLen)
	}
	if mem1, _, extra1 := Check(); mem1 != mem0+1 || extra1 != extra0 {
		t.Fatalf("DIO pool counters: mem %d -> %d, extra %d -> %d", mem0, mem1, extra0, extra1)
	}
	FreeFB(fb)
	if mem1, _, extra1 := Check(); mem1 != mem0 || extra1 != extra0 {
		t.Fatalf("DIO pool leaked: mem %d -> %d, extra %d -> %d", mem0, mem1, extra0, extra1)
	}
}

func TestDioConcurrent(t *testing.T) {
	mem0, buf0, extra0 := Check()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				fb, err := NewDioFB(5000, 4096)
				if err != nil {
					t.Error(err)
					return
				}
				fb.GetBuf()[0] = byte(i)
				w, err := WrapDio(fb.GetBuf(), 4096)
				if err != nil {
					FreeFB(fb)
					t.Error(err)
					return
				}
				FreeFB(w)
				FreeFB(fb)
				n := NewFB(4096)
				FreeFB(n)
			}
		}()
	}
	wg.Wait()
	mem1, buf1, extra1 := Check()
	if mem1 != mem0 || buf1 != buf0 || extra1 != extra0 {
		t.Fatalf("counters leaked: mem %d->%d buf %d->%d extra %d->%d", mem0, mem1, buf0, buf1, extra0, extra1)
	}
}

func TestDioWritePath(t *testing.T) {
	fb, err := NewDioFB(8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0xAB}, 4096)
	if n, err := fb.Write(payload); n != 4096 || err != nil {
		t.Fatalf("Write: %d %v", n, err)
	}
	if !baseAligned(fb.Bytes(), 4096) {
		t.Fatal("Bytes() base not aligned at woff=0")
	}
	if err := fb.ReSize(0, 4096); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if n, err := fb.WriteTo(&out); n != 4096 || err != nil {
		t.Fatalf("WriteTo: %d %v", n, err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatal("data mismatch")
	}
	FreeFB(fb)
}

func TestDioReadFrom(t *testing.T) {
	for _, size := range []int{4096, 8192} {
		fb, err := NewDioFB(8192, 4096)
		if err != nil {
			t.Fatal(err)
		}
		payload := bytes.Repeat([]byte{0xCD}, size)
		n, err := fb.ReadFrom(bytes.NewReader(payload))
		if n != int64(size) || err != nil {
			t.Fatalf("size %d: ReadFrom %d %v", size, n, err)
		}
		if fb.GetReadOffset() != size {
			t.Fatalf("size %d: roff %d", size, fb.GetReadOffset())
		}
		if !baseAligned(fb.Bytes(), 4096) || !bytes.Equal(fb.Bytes(), payload) {
			t.Fatalf("size %d: Bytes view wrong", size)
		}
		FreeFB(fb)
	}
}

func TestDioReSizeKeepsBase(t *testing.T) {
	fb, err := NewDioFB(16384, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := fb.ReSize(0, 8192); err != nil {
		t.Fatal(err)
	}
	if !baseAligned(fb.GetBuf(), 4096) {
		t.Fatal("ReSize(0,n) broke base alignment")
	}
	FreeFB(fb)
}

func TestDioFreeReuseMixed(t *testing.T) {
	mem0, buf0, extra0 := Check()
	for i := 0; i < 1000; i++ {
		dio, err := NewDioFB(5000, 4096)
		if err != nil {
			t.Fatal(err)
		}
		norm := NewFB(8192)
		FreeFB(dio)
		FreeFB(norm)
	}
	mem1, buf1, extra1 := Check()
	if mem1 != mem0 || buf1 != buf0 || extra1 != extra0 {
		t.Fatalf("counters leaked: mem %d->%d buf %d->%d extra %d->%d", mem0, mem1, buf0, buf1, extra0, extra1)
	}
	// DIO(5000,4096) uses an exact-size DIO pool; verify the 16384 normal
	// slab is unpolluted.
	// The cap check catches cross-pool routing errors; FreeFB owns the
	// DIO slab and must not place the narrowed view back into either pool.
	fb := NewFB(16384)
	if cap(fb.GetBuf()) != 16384 {
		t.Fatalf("pool polluted: cap %d", cap(fb.GetBuf()))
	}
	FreeFB(fb)
}

func TestWrapDio(t *testing.T) {
	raw := Allocate(16384)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	data := raw[off : off+8192 : off+8192]
	data[0] = 0x5A

	mem0, buf0, _ := Check()
	fb, err := WrapDio(data, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if &fb.GetBuf()[0] != &data[0] {
		t.Fatal("WrapDio copied instead of wrapping")
	}
	fb.Reset(len(data), 0)
	if !baseAligned(fb.Bytes(), 4096) || fb.Bytes()[0] != 0x5A {
		t.Fatal("wrapped data view wrong")
	}
	FreeFB(fb)
	if data[0] != 0x5A {
		t.Fatal("FreeFB touched wrapped data")
	}
	mem1, buf1, _ := Check()
	if mem1 != mem0 || buf1 != buf0 {
		t.Fatalf("wrap counters wrong: mem %d->%d buf %d->%d", mem0, mem1, buf0, buf1)
	}
	Free(raw)
}

func TestWrapDioClampsCap(t *testing.T) {
	raw := Allocate(8192)
	defer Free(raw)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	data := raw[off : off+4096]

	fb, err := WrapDio(data, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if cap(fb.GetBuf()) != 4096 {
		t.Fatalf("wrapped cap = %d, want 4096", cap(fb.GetBuf()))
	}
	FreeFB(fb)
}

func TestDioDebugTrace(t *testing.T) {
	SetDebuger(6)
	defer SetDebuger(0)
	fb, err := NewDioFB(4096, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := fb.Reset(1, 0); err != nil {
		t.Fatal(err)
	}
	fb.Bytes() // triggers the unaligned-Bytes debug warning
	FreeFB(fb)
	raw := Allocate(8192)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	w, err := WrapDio(raw[off:off+4096], 4096)
	if err != nil {
		t.Fatal(err)
	}
	FreeFB(w)
	Free(raw)
}

func TestWrapDioRejects(t *testing.T) {
	raw := Allocate(8192)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	aligned := raw[off : off+4096]
	if _, err := WrapDio(aligned, 3); err == nil {
		t.Fatal("bad align accepted")
	}
	if _, err := WrapDio(nil, 4096); err == nil {
		t.Fatal("empty data accepted")
	}
	// slab base is at least 8-aligned, so base+1 is odd and never 4096-aligned
	if _, err := WrapDio(raw[1:], 4096); err == nil {
		t.Fatal("misaligned base accepted")
	}
	if _, err := WrapDio(aligned[:4095], 4096); err == nil {
		t.Fatal("unaligned length accepted")
	}
	Free(raw)
}

func TestDioDoubleFree(t *testing.T) {
	fb, err := NewDioFB(4096, 4096)
	if err != nil {
		t.Fatal(err)
	}
	FreeFB(fb)
	mem0, buf0, _ := Check()
	FreeFB(fb) // must warn and early-return, no second Put, no counter drift
	mem1, buf1, _ := Check()
	if mem1 != mem0 || buf1 != buf0 {
		t.Fatalf("double free drifted counters: mem %d->%d buf %d->%d", mem0, mem1, buf0, buf1)
	}
	n := NewFB(16384)
	if cap(n.GetBuf()) != 16384 {
		t.Fatalf("pool polluted after double free: cap %d", cap(n.GetBuf()))
	}
	FreeFB(n)
}

func TestGetFBInuseGuard(t *testing.T) {
	fb := NewFB(4096)
	FreeFB(fb)
	fastBufferPool.Put(fb) // simulate a double Put: same pointer twice in pool
	a := NewFB(4096)
	b := NewFB(4096) // must hit the inuse guard and fall through to a fresh fb
	if a == b {
		t.Fatal("inuse guard handed out the same live fb twice")
	}
	FreeFB(a)
	FreeFB(b)
}

func BenchmarkNewFB4K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fb := NewFB(4096)
		FreeFB(fb)
	}
}

// DIO uses exact-size pools, so these do not jump to the next power-of-two
// slab size the way the first implementation did.
func BenchmarkNewDioFB4K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fb, err := NewDioFB(4096, 4096)
		if err != nil {
			b.Fatal(err)
		}
		FreeFB(fb)
	}
}

func BenchmarkNewFB4M(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fb := NewFB(4 * MBSize)
		FreeFB(fb)
	}
}

func BenchmarkNewDioFB4M(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fb, err := NewDioFB(4*MBSize, 4096)
		if err != nil {
			b.Fatal(err)
		}
		FreeFB(fb)
	}
}

func BenchmarkWrapDio4K(b *testing.B) {
	raw := Allocate(8192)
	defer Free(raw)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((4096 - base%4096) % 4096)
	data := raw[off : off+4096 : off+4096]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fb, err := WrapDio(data, 4096)
		if err != nil {
			b.Fatal(err)
		}
		FreeFB(fb)
	}
}
