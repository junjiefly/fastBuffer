package fastBuffer

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var bufTotal int64
var extraTotal int64
var debugLevel int

var logPrintln func(args ...interface{})
var logPrintf func(format string, args ...interface{})

func InitLogger(println func(args ...interface{}), printf func(format string, args ...interface{})) {
	logPrintln = println
	logPrintf = printf
}

func SetDebuger(lv int) {
	debugLevel = lv
	logPrintln("fast buffer debug level changes to", lv)
}

//check leak
func Check() (memCnt int64, boolCnt int64, extraCnt int64) {
	return atomic.LoadInt64(&memTotal), atomic.LoadInt64(&bufTotal), atomic.LoadInt64(&extraTotal)
}

var fastBufferPool = sync.Pool{
	New: func() interface{} {
		return new(FastBuffer)
	},
}

func NewFB(size int) *FastBuffer {
	var fb *FastBuffer
	for {
		fb = fastBufferPool.Get().(*FastBuffer)
		if fb.inuse == true {
			_, file, line, ok := runtime.Caller(2)
			if i := strings.LastIndex(file, "/"); i >= 0 {
				file = file[i+1:]
			}
			logPrintf("Bug warning! in use fb is being reused! this fb has been freed more than once in somewhere. buf:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v\n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
			continue
		}
		break
	}
	fb.allocate(size)
	return fb
}

//you called NewFB(), and you must call FreeFB()
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
	roff  int    //how many bytes have been readed to fb from other user
	woff  int    //how many bytes have been sended to other user
	inuse bool   //is this fb being used?
}

func (fb *FastBuffer) GetBuf() []byte {
	return fb.buf
}

func (fb *FastBuffer) allocate(size int) {
	fb.buf = Allocate(size)
	atomic.AddInt64(&bufTotal, 1)
	if debugLevel&2 > 0 {
		_, file, line, ok := runtime.Caller(2)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT ++ add buf:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v \n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
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
	if fb.buf != nil {
		Free(fb.buf)
	}
	atomic.AddInt64(&bufTotal, -1)
	if debugLevel&2 > 0 {
		_, file, line, ok := runtime.Caller(2)
		if i := strings.LastIndex(file, "/"); i >= 0 {
			file = file[i+1:]
		}
		logPrintf("TT -- del buf:%d inuse:%v roff:%d woff:%d file:%s line:%d ptr:%p ok:%v\n", bufTotal, fb.inuse, fb.roff, fb.woff, file, line, fb, ok)
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
	} else {
		return fb.buf[fb.woff:fb.roff]
	}
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

//read data to fb data
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

//copy fb data to other user
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

//copy data from other user
func (fb *FastBuffer) CopyFrom(p []byte) (n int, err error) {
	if len(p) > len(fb.buf) {
		return 0, io.ErrShortBuffer
	}
	m := copy(fb.buf[fb.roff:], p)
	fb.roff += m
	return m, nil
}

//send fb data to other user
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
