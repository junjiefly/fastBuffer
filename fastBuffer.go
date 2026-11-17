package fastBuffer

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

var bufTotal int64
var extraTotal int64
var debugLevelValue int64

type logger struct {
	println func(args ...interface{})
	printf  func(format string, args ...interface{})
}

var noopLogger = logger{
	println: func(args ...interface{}) {},
	printf:  func(format string, args ...interface{}) {},
}

var currentLogger atomic.Value

func init() {
	currentLogger.Store(noopLogger)
}

func getLogger() logger {
	if value := currentLogger.Load(); value != nil {
		if logger, ok := value.(logger); ok {
			return logger
		}
	}
	return noopLogger
}

func logPrintln(args ...interface{}) {
	getLogger().println(args...)
}

func logPrintf(format string, args ...interface{}) {
	getLogger().printf(format, args...)
}

func InitLogger(println func(args ...interface{}), printf func(format string, args ...interface{})) {
	if println == nil {
		println = func(args ...interface{}) {}
	}
	if printf == nil {
		printf = func(format string, args ...interface{}) {}
	}
	currentLogger.Store(logger{println: println, printf: printf})
}

func debugLevel() int64 {
	return atomic.LoadInt64(&debugLevelValue)
}

func SetDebuger(lv int) {
	atomic.StoreInt64(&debugLevelValue, int64(lv))
	logPrintln("fast buffer debug level changes to", lv)
}

// check leak
func Check() (memCnt int64, boolCnt int64, extraCnt int64) {
	return atomic.LoadInt64(&memTotal), atomic.LoadInt64(&bufTotal), atomic.LoadInt64(&extraTotal)
}

var fastBufferPool = sync.Pool{
	New: func() interface{} {
		return new(FastBuffer)
	},
}

func getFB() *FastBuffer {
	for {
		fb := fastBufferPool.Get().(*FastBuffer)
		if fb.inuse == true {
			_, file, line, ok := runtime.Caller(2)
			if i := strings.LastIndex(file, "/"); i >= 0 {
				file = file[i+1:]
			}
			logPrintf("Bug warning! in use fb is being reused! this fb has been freed more than once in somewhere. buf:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v\n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
			continue
		}
		return fb
	}
}

func NewFB(size int) *FastBuffer {
	fb := getFB()
	fb.allocate(size)
	return fb
}

// NewDioFB returns a buffer whose base address and length are multiples of
// align (a power of two), suitable for O_DIRECT / FILE_FLAG_NO_BUFFERING.
// macOS has no true O_DIRECT; there the buffer pairs with fcntl(F_NOCACHE),
// which disables caching but does not enforce alignment.
// Release it with FreeFB only. Never pass GetBuf() to Free(): the returned
// slice is an aligned view into a larger DIO slab, and freeing it directly
// corrupts the DIO pools.
func NewDioFB(size, align int) (*FastBuffer, error) {
	if size <= 0 {
		return nil, errors.New("size must be positive")
	}
	if align <= 0 || align&(align-1) != 0 {
		return nil, errors.New("align must be a power of two")
	}
	if align > max_size {
		return nil, errors.New("align exceeds max slab size")
	}
	const maxInt = int(^uint(0) >> 1)
	// keep rounded+align from overflowing int
	if size > maxInt-2*align {
		return nil, errors.New("size too large")
	}
	rounded := (size + align - 1) &^ (align - 1)
	raw := allocateDio(rounded + align)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := int((uintptr(align) - base%uintptr(align)) % uintptr(align))
	fb := getFB()
	fb.raw = raw
	fb.buf = raw[off : off+rounded : off+rounded]
	fb.align = align
	atomic.AddInt64(&bufTotal, 1)
	if debugLevel()&2 > 0 {
		_, file, line, ok := runtime.Caller(1)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT fb ++, total:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v \n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
	}
	fb.inuse = true
	return fb, nil
}

// WrapDio wraps externally owned, already aligned memory (e.g. an RDMA
// registered slot) as a DIO buffer. FreeFB only returns the FastBuffer
// struct; the data is never freed or pooled. The owner must keep the data
// alive and unchanged until all in-flight I/O on it completes.
func WrapDio(data []byte, align int) (*FastBuffer, error) {
	if align <= 0 || align&(align-1) != 0 {
		return nil, errors.New("align must be a power of two")
	}
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}
	if uintptr(unsafe.Pointer(&data[0]))%uintptr(align) != 0 {
		return nil, errors.New("data base address is not aligned")
	}
	if len(data)%align != 0 {
		return nil, errors.New("data length is not a multiple of align")
	}
	fb := getFB()
	fb.buf = data[:len(data):len(data)]
	fb.raw = nil
	fb.align = align
	atomic.AddInt64(&bufTotal, 1)
	if debugLevel()&2 > 0 {
		_, file, line, ok := runtime.Caller(1)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT fb ++, total:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v \n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
	}
	fb.inuse = true
	return fb, nil
}

// you called NewFB(), and you must call FreeFB()
func FreeFB(fb *FastBuffer) {
	if fb == nil {
		return
	}
	if fb.inuse == false {
		_, file, line, ok := runtime.Caller(2)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("Bug warning! a fb cannot be freed when it is not in used. buf:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v\n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
		return
	}
	fb.free()
	fastBufferPool.Put(fb)
}

type FastBuffer struct {
	buf   []byte //data bytes
	raw   []byte //original slab backing buf for DIO buffers; nil otherwise
	align int    //0 for normal buffers, power of two for DIO buffers
	roff  int    //how many bytes have been readed to fb from other user
	woff  int    //how many bytes have been sended to other user
	inuse bool   //is this fb being used?
}

// GetBuf returns the whole buffer starting at the (DIO-aligned) base.
// For DIO buffers the result is a view into a larger slab: release the
// FastBuffer with FreeFB, never call Free() on this slice.
func (fb *FastBuffer) GetBuf() []byte {
	return fb.buf
}

// Align returns the DIO alignment, or 0 for normal buffers.
func (fb *FastBuffer) Align() int {
	return fb.align
}

func (fb *FastBuffer) allocate(size int) {
	fb.buf = Allocate(size)
	atomic.AddInt64(&bufTotal, 1)
	if debugLevel()&2 > 0 {
		_, file, line, ok := runtime.Caller(2)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT fb ++, total:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v \n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
	}
	fb.inuse = true
}

func (fb *FastBuffer) reset() {
	if fb.buf != nil {
		fb.buf = fb.buf[:0]
	}
	fb.roff = 0
	fb.woff = 0
}

func (fb *FastBuffer) free() {
	fb.reset()
	if fb.align > 0 {
		if fb.raw != nil {
			freeDio(fb.raw)
			fb.raw = nil
		}
		// wrapped data is not owned, nothing to release
		fb.buf = nil
		fb.align = 0
	} else if fb.buf != nil {
		Free(fb.buf)
		fb.buf = nil
	}
	atomic.AddInt64(&bufTotal, -1)
	if debugLevel()&2 > 0 {
		_, file, line, ok := runtime.Caller(2)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT fb --, total:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v\n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
	}
	fb.inuse = false
}

func (fb *FastBuffer) ReSize(start, newLength int) error {
	if fb.buf == nil {
		return errors.New("buffer empty")
	}
	length := len(fb.buf)
	if start > length || (start+newLength) > length {
		return io.ErrShortBuffer
	}
	if start == 0 {
		fb.buf = fb.buf[:newLength]
		fb.roff = newLength
	} else {
		fb.buf = fb.buf[0 : start+newLength]
		fb.woff = start
		fb.roff = start + newLength
	}
	return nil
}

func (fb *FastBuffer) Reset(roff, woff int) error {
	if fb.buf == nil {
		return errors.New("buffer empty")
	}
	length := len(fb.buf)
	if roff > length || woff > length {
		return io.ErrShortBuffer
	}
	if roff >= 0 {
		fb.roff = roff
	}
	if woff >= 0 {
		fb.woff = woff
	}
	return nil
}

func (fb *FastBuffer) Bytes() []byte {
	if fb.buf == nil {
		return nil
	}
	if debugLevel()&4 > 0 && fb.align > 0 && (fb.woff%fb.align != 0 || (fb.roff-fb.woff)%fb.align != 0) {
		logPrintf("Dio warning! Bytes() not aligned: align:%d woff:%d roff:%d ptr:%p\n", fb.align, fb.woff, fb.roff, fb)
	}
	return fb.buf[fb.woff:fb.roff]
}

func (fb *FastBuffer) empty() bool {
	if fb.buf == nil {
		return true
	} else {
		return len(fb.buf) <= fb.woff
	}
}

func (fb *FastBuffer) Len() int {
	if fb.buf == nil {
		return 0
	} else {
		return len(fb.buf) - fb.woff
	}
}

func (fb *FastBuffer) GetReadOffset() int {
	if fb.buf == nil {
		return 0
	} else {
		return fb.roff
	}
}

// read data to fb data
func (fb *FastBuffer) ReadFrom(r io.Reader) (int64, error) {
	if fb.buf == nil {
		return 0, errors.New("buffer empty")
	}
	var sum int64
	capInt := len(fb.buf)
	capInt64 := int64(capInt)
	for {
		m, e := r.Read(fb.buf[fb.roff:capInt])
		if m < 0 {
			return 0, errors.New("reader returned negative count from Read")
		}
		if m == 0 {
			_, e = io.Copy(io.Discard, r)
			if e != nil && e != io.EOF {
				return sum, e
			}
			return sum, nil
		}
		fb.roff += m
		sum += int64(m)
		if sum >= capInt64 {
			_, e = io.Copy(io.Discard, r)
			if e != nil && e != io.EOF {
				return sum, e
			}
			return sum, nil
		}
		if e == io.EOF {
			return sum, nil // e is EOF, so return nil explicitly
		}
		if e != nil {
			return sum, e
		}
	}
}

// copy fb data to other user
func (fb *FastBuffer) Read(p []byte) (n int, err error) {
	if fb.empty() {
		// Buffer is empty, reset to recover space.
		fb.reset()
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n = copy(p, fb.buf[fb.woff:])
	fb.woff += n
	return n, nil
}

// copy data from other user
func (fb *FastBuffer) CopyFrom(p []byte) (n int, err error) {
	if len(p) > len(fb.buf) {
		return 0, io.ErrShortBuffer
	}
	m := copy(fb.buf[fb.roff:], p)
	fb.roff += m
	return m, nil
}

// send fb data to other user
func (fb *FastBuffer) WriteTo(w io.Writer) (n int64, err error) {
	if fb.buf == nil {
		return 0, nil
	}
	if nBytes := fb.Len(); nBytes > 0 {
		m, e := w.Write(fb.buf[fb.woff:])
		if m > nBytes {
			return 0, errors.New("invalid write count")
		}
		fb.woff += m
		n = int64(m)
		if e != nil {
			return n, e
		}
		// all bytes should have been written, by definition of
		// Write method in io.Writer
		if m != nBytes {
			return n, io.ErrShortWrite
		}
	}
	// Buffer is now empty; reset.
	fb.reset()
	return n, nil
}

func (fb *FastBuffer) Write(buf []byte) (n int, err error) {
	return fb.CopyFrom(buf)
}

func (fb *FastBuffer) WriteByte(b byte) (n int, err error) {
	if 1 > len(fb.buf) {
		return 0, io.ErrShortBuffer
	}
	fb.buf[fb.roff] = b
	fb.roff++
	return 1, nil
}
