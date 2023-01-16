package fastBuffer

import (
	"bytes"
	"fmt"
	"testing"
)

var KBBuf []byte
var KB4Buf []byte
var KB16Buf []byte
var KB128Buf []byte
var KB512Buf []byte
var MBBuf []byte
var MB4Buf []byte

func TestMain(m *testing.M) {
	fmt.Println("main")
	KBBuf = make([]byte, KBSize)
	KB4Buf = make([]byte, 4*KBSize)
	KB16Buf = make([]byte, 16*KBSize)
	KB128Buf = make([]byte, 128*KBSize)
	KB512Buf = make([]byte, 512*KBSize)
	MB4Buf = make([]byte, 4*MBSize)
	MBBuf = make([]byte, MBSize)

	var myPrintln = func(args ...interface{}) {
		fmt.Println(args...)
	}
	var myPrintf = func(format string, args ...interface{}) {
		fmt.Printf(format, args...)
	}
	InitLogger(myPrintln, myPrintf)
	m.Run()
}

func TestAllocate1KB(t *testing.T) {
	fb := NewFB(KBSize)
	FreeFB(fb)
}

func BenchmarkRead1KB(b *testing.B) {
	buffer := bytes.NewBuffer(KBBuf)
	fb := NewFB(KBSize)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead4KB(b *testing.B) {
	buffer := bytes.NewBuffer(KB4Buf)
	fb := NewFB(KBSize * 4)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead16KB(b *testing.B) {
	buffer := bytes.NewBuffer(KB16Buf)
	fb := NewFB(KBSize * 16)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead128KB(b *testing.B) {
	buffer := bytes.NewBuffer(KB128Buf)
	fb := NewFB(KBSize * 128)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead512KB(b *testing.B) {
	buffer := bytes.NewBuffer(KB512Buf)
	fb := NewFB(KBSize * 512)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead1MB(b *testing.B) {
	buffer := bytes.NewBuffer(MBBuf)
	fb := NewFB(MBSize)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}

func BenchmarkRead4MB(b *testing.B) {
	buffer := bytes.NewBuffer(MB4Buf)
	fb := NewFB(MBSize * 4)
	_, _ = fb.ReadFrom(buffer)
	FreeFB(fb)
}
