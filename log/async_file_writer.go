package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// / Aligned ticker produces a tick every time the current number of seconds
// / is divisible by a number of seconds corresponding to a given duration.
type AlignedTicker struct {
	stop chan struct{}
	C    <-chan time.Time
}

func NewAlignedTicker(duration time.Duration) *AlignedTicker {
	ht := &AlignedTicker{
		stop: make(chan struct{}),
	}
	ht.C = ht.Ticker(duration)
	return ht
}

func (ht *AlignedTicker) Stop() {
	ht.stop <- struct{}{}
}

func (ht *AlignedTicker) Ticker(duration time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	go func() {
		intervalLow := time.Now().Unix() / int64(duration.Seconds())
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case t := <-ticker.C:
				intervalTick := t.Unix() / int64(duration.Seconds())
				if intervalLow != intervalTick {
					intervalLow = intervalTick
					t = time.Unix(intervalLow*int64(duration.Seconds()), 0)
					ch <- t
				}
			case <-ht.stop:
				return
			}
		}
	}()
	return ch
}

type AsyncFileWriter struct {
	filePath string
	fd       *os.File

	wg      sync.WaitGroup
	started int32
	buf     chan []byte
	stop    chan struct{}
	ticker  *AlignedTicker
}

func NewAsyncFileWriter(filePath string, bufSize int64, duration time.Duration) *AsyncFileWriter {
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		panic(fmt.Sprintf("get file path of logger error. filePath=%s, err=%s", filePath, err))
	}

	return &AsyncFileWriter{
		filePath: absFilePath,
		buf:      make(chan []byte, bufSize),
		stop:     make(chan struct{}),
		ticker:   NewAlignedTicker(duration),
	}
}

func (w *AsyncFileWriter) initLogFile(t time.Time) error {
	var (
		fd  *os.File
		err error
	)

	realFilePath := w.timeFilePath(w.filePath, t)
	fd, err = os.OpenFile(realFilePath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	w.fd = fd
	_, err = os.Lstat(w.filePath)
	if err == nil || os.IsExist(err) {
		err = os.Remove(w.filePath)
		if err != nil {
			return err
		}
	}

	err = os.Symlink(realFilePath, w.filePath)
	if err != nil {
		return err
	}

	return nil
}

func (w *AsyncFileWriter) Start() error {
	if !atomic.CompareAndSwapInt32(&w.started, 0, 1) {
		return errors.New("logger has already been started")
	}

	err := w.initLogFile(time.Now())
	if err != nil {
		return err
	}

	w.wg.Add(1)
	go func() {
		defer func() {
			atomic.StoreInt32(&w.started, 0)

			w.flushBuffer()
			w.flushAndClose()

			w.wg.Done()
		}()

		for {
			select {
			case msg, ok := <-w.buf:
				if !ok {
					fmt.Fprintln(os.Stderr, "buf channel has been closed.")
					return
				}
				w.SyncWrite(msg)
			case <-w.stop:
				return
			}
		}
	}()
	return nil
}

func (w *AsyncFileWriter) flushBuffer() {
	for {
		select {
		case msg := <-w.buf:
			w.SyncWrite(msg)
		default:
			return
		}
	}
}

func (w *AsyncFileWriter) SyncWrite(msg []byte) {
	w.rotateFile()
	if w.fd != nil {
		w.fd.Write(msg)
	}
}

func (w *AsyncFileWriter) rotateFile() {
	select {
	case t := <-w.ticker.C:
		if err := w.flushAndClose(); err != nil {
			fmt.Fprintf(os.Stderr, "flush and close file error. err=%s", err)
		}
		if err := w.initLogFile(t); err != nil {
			fmt.Fprintf(os.Stderr, "init log file error. err=%s", err)
		}
	default:
	}
}

func (w *AsyncFileWriter) Stop() {
	w.stop <- struct{}{}
	w.wg.Wait()

	w.ticker.Stop()
}

func (w *AsyncFileWriter) Write(msg []byte) (n int, err error) {
	// TODO(wuzhenxing): for the underlying array may change, is there a better way to avoid copying slice?
	buf := make([]byte, len(msg))
	copy(buf, msg)

	select {
	case w.buf <- buf:
	default:
	}
	return 0, nil
}

func (w *AsyncFileWriter) Flush() error {
	if w.fd == nil {
		return nil
	}
	return w.fd.Sync()
}

func (w *AsyncFileWriter) flushAndClose() error {
	if w.fd == nil {
		return nil
	}

	err := w.fd.Sync()
	if err != nil {
		return err
	}

	return w.fd.Close()
}

func (w *AsyncFileWriter) timeFilePath(filePath string, t time.Time) string {
	return filePath + "." + t.Format("2006-01-02_15:04:05")
}
