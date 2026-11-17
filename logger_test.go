package fastBuffer

import (
	"sync"
	"testing"
)

func TestInitLoggerNilCallbacksAreNoOp(t *testing.T) {
	oldLogger := getLogger()
	t.Cleanup(func() {
		currentLogger.Store(oldLogger)
	})

	InitLogger(nil, nil)
	logPrintln("no-op")
	logPrintf("no-op %d", 1)
}

func TestLoggerConcurrentInitAndUse(t *testing.T) {
	oldLogger := getLogger()
	t.Cleanup(func() {
		currentLogger.Store(oldLogger)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				logPrintf("logger race %d", j)
				logPrintln("logger race")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				InitLogger(nil, nil)
			}
		}()
	}
	wg.Wait()
}

func TestSetDebugerConcurrent(t *testing.T) {
	oldLogger := getLogger()
	t.Cleanup(func() {
		currentLogger.Store(oldLogger)
		SetDebuger(0)
	})
	InitLogger(nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if debugLevel() < 0 {
					t.Error("debug level became negative")
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				SetDebuger(j & 7)
			}
		}()
	}
	wg.Wait()
}
