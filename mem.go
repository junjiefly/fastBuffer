package fastBuffer

import (
	"fmt"
	memset "github.com/tmthrgd/go-memset"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var memTotal int64

var memPools []*sync.Pool

const KBSize = 1024
const MBSize = KBSize * KBSize

const (
	min_size = 8                // min size will be included in memPools
	max_size = 64 * 1024 * 1024 // max size will not be included in memPools
)

var capMap map[int]struct{}

func getCount(size int) (count int) {
	for count = 0; size > min_size; count++ {
		size = (size + 1) >> 1
	}
	return
}

func init() {
	capMap = make(map[int]struct{})
	memPools = make([]*sync.Pool, getCount(int(max_size))) // 8~32MB
	for i := 0; i < len(memPools); i++ {
		memSize := min_size << i
		capMap[memSize] = struct{}{}
		memPools[i] = &sync.Pool{
			New: func() interface{} {
				buffer := make([]byte, memSize)
				return &buffer
			},
		}
	}
}

func getMemPool(size int) *sync.Pool {
	index := getCount(size)
	if index >= len(memPools) {
		return nil
	}
	return memPools[index]
}

func Memset(buf []byte) {
	if buf == nil {
		return
	}
	caps := cap(buf)
	if caps == 0 {
		return
	}
	buf = buf[:caps]
	memset.Memset(buf, 0)
}

//get a mem block, but may have dirty data
//you should notice the length and avoid to read dirty data
func Allocate(size int) []byte {
	if pool := getMemPool(size); pool != nil {
		atomic.AddInt64(&memTotal, 1)
		slab := *pool.Get().(*[]byte)

		if debugLevel&1 > 0 {
			_, file, line, ok := runtime.Caller(2)
			if i := strings.LastIndex(file, "/"); i >= 0 {
				file = file[i+1:]
			}
			logPrintf("TT mem ++, total:%d file:%s line:%d ok:%v\n", memTotal, file, line, ok)
		}
		return slab[:size]
	}
	atomic.AddInt64(&extraTotal, 1)
	fmt.Println("extra:", size)
	return make([]byte, size)
}

//get a mem block with no dirty data
//but memset will take some costs
func AllocateNew(size int) []byte {
	if pool := getMemPool(size); pool != nil {
		atomic.AddInt64(&memTotal, 1)
		slab := *pool.Get().(*[]byte)
		Memset(slab)
		if debugLevel&1 > 0 {
			_, file, line, ok := runtime.Caller(2)
			if i := strings.LastIndex(file, "/"); i >= 0 {
				file = file[i+1:]
			}
			logPrintf("TT mem ++, total:%d file:%s line:%d ok:%v\n", memTotal, file, line, ok)
		}
		return slab[:size]
	}
	atomic.AddInt64(&extraTotal, 1)
	return make([]byte, size)
}

//you called Allocate()/AllocateNew(), and you must call Free()
func Free(buf []byte) {
	bufCap := cap(buf)
	if _, ok := capMap[bufCap]; !ok {
		logPrintln("TT invalid cap size size:", bufCap)
		return
	}
	if pool := getMemPool(bufCap); pool != nil {
		atomic.AddInt64(&memTotal, -1)
		if debugLevel&1 > 0 {
			_, file, line, ok := runtime.Caller(2)
			if i := strings.LastIndex(file, "/"); i >= 0 {
				file = file[i+1:]
			}
			logPrintf("TT mem --, total:%d file:%s line:%d ok:%v\n", memTotal, file, line, ok)
		}
		pool.Put(&buf)
	}
}
