package fastBuffer

import (
	"sync"
	"sync/atomic"
)

// DIO allocations need one alignment window in addition to the rounded data
// length. Keeping them outside the power-of-two slab pools avoids jumping to
// the next size class; 4MB + 4KB stays at its requested size instead of
// consuming an 8MB slab.
var dioPools sync.Map

func getDioPool(size int) *sync.Pool {
	if pool, ok := dioPools.Load(size); ok {
		return pool.(*sync.Pool)
	}
	pool := &sync.Pool{
		New: func() interface{} {
			buffer := make([]byte, size)
			return &buffer
		},
	}
	actual, _ := dioPools.LoadOrStore(size, pool)
	return actual.(*sync.Pool)
}

func allocateDio(size int) []byte {
	slab := *getDioPool(size).Get().(*[]byte)
	atomic.AddInt64(&memTotal, 1)
	return slab[:size]
}

func freeDio(buf []byte) {
	if cap(buf) == 0 {
		return
	}
	getDioPool(cap(buf)).Put(&buf)
	atomic.AddInt64(&memTotal, -1)
}
