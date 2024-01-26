package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const backupTimeFormat = "2006-01-02_15"

type TimeTicker struct {
	stop chan struct{}
	C    <-chan time.Time
}

// NewTimeTicker creates a TimeTicker that notifies based on rotateHours parameter.
// if rotateHours is 1 and current time is 11:32 it means that the ticker will tick at 12:00
// if rotateHours is 2 and current time is 09:12 means that the ticker will tick at 11:00
// specially, if rotateHours is 0, then no rotation
func NewTimeTicker(rotateHours uint) *TimeTicker {
	ch := make(chan time.Time)
	tt := TimeTicker{
		stop: make(chan struct{}),
		C:    ch,
	}

	if rotateHours > 0 {
		tt.startTicker(ch, rotateHours)
	}

	return &tt
}

func (tt *TimeTicker) Stop() {
	tt.stop <- struct{}{}
}

func (tt *TimeTicker) startTicker(ch chan time.Time, rotateHours uint) {
	go func() {
		nextRotationHour := getNextRotationHour(time.Now(), rotateHours)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case t := <-ticker.C:
				if t.Hour() == nextRotationHour {
					ch <- t
					nextRotationHour = getNextRotationHour(time.Now(), rotateHours)
				}
			case <-tt.stop:
				return
			}
		}
	}()
}

func getNextRotationHour(now time.Time, delta uint) int {
	return now.Add(time.Hour * time.Duration(delta)).Hour()
}

type AsyncFileWriter struct {
	filePath string
	fd       *os.File

	wg         sync.WaitGroup
	started    int32
	buf        chan []byte
	stop       chan struct{}
	timeTicker *TimeTicker

	maxBackups int
}

func NewAsyncFileWriter(filePath string, maxBytesSize int64, maxBackups int, rotateHours uint) *AsyncFileWriter {
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		panic(fmt.Sprintf("get file path of logger error. filePath=%s, err=%s", filePath, err))
	}

	return &AsyncFileWriter{
		filePath:   absFilePath,
		buf:        make(chan []byte, maxBytesSize),
		stop:       make(chan struct{}),
		maxBackups: maxBackups,
		timeTicker: NewTimeTicker(rotateHours),
	}
}

func (w *AsyncFileWriter) initLogFile() error {
	var (
		fd  *os.File
		err error
	)

	realFilePath := w.timeFilePath(w.filePath)
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

	_ = w.clearBackups()

	return nil
}

func (w *AsyncFileWriter) Start() error {
	if !atomic.CompareAndSwapInt32(&w.started, 0, 1) {
		return errors.New("logger has already been started")
	}

	err := w.initLogFile()
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
	case <-w.timeTicker.C:
		if err := w.flushAndClose(); err != nil {
			fmt.Fprintf(os.Stderr, "flush and close file error. err=%s", err)
		}
		if err := w.initLogFile(); err != nil {
			fmt.Fprintf(os.Stderr, "init log file error. err=%s", err)
		}
	default:
	}
}

func (w *AsyncFileWriter) Stop() {
	w.stop <- struct{}{}
	w.wg.Wait()

	w.timeTicker.Stop()
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

func (w *AsyncFileWriter) timeFilePath(filePath string) string {
	return filePath + "." + time.Now().Format(backupTimeFormat)
}

func (w *AsyncFileWriter) dir() string {
	return filepath.Dir(w.filePath)
}

// oldLogFiles returns the list of backup log files stored in the same
// directory as the current log file, sorted by ModTime
func (w *AsyncFileWriter) oldLogFiles() ([]logInfo, error) {
	files, err := os.ReadDir(w.dir())
	if err != nil {
		return nil, fmt.Errorf("can't read log file directory: %s", err)
	}
	logFiles := []logInfo{}

	prefix := filepath.Base(w.filePath)

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		k := f.Name()
		if t, err := w.timeFromName(k, prefix); err == nil {
			logFiles = append(logFiles, logInfo{t, f})
		}
	}

	sort.Sort(byFormatTime(logFiles))

	return logFiles, nil
}

// logInfo is a convenience struct to return the filename and its embedded
// timestamp.
type logInfo struct {
	timestamp time.Time
	fs.DirEntry
}

// byFormatTime sorts by newest time formatted in the name.
type byFormatTime []logInfo

func (b byFormatTime) Less(i, j int) bool {
	return b[i].timestamp.After(b[j].timestamp)
}

func (b byFormatTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byFormatTime) Len() int {
	return len(b)
}

func (w *AsyncFileWriter) timeFromName(filename, prefix string) (time.Time, error) {
	if !strings.HasPrefix(filename, prefix) || len(filename) == len(prefix) {
		return time.Time{}, errors.New("mismatched prefix")
	}
	ts := filename[len(prefix)+1:]
	a, _ := time.Parse(backupTimeFormat, ts)
	return a, nil
}

func (w *AsyncFileWriter) clearBackups() error {
	if w.maxBackups == 0 {
		return nil
	}
	files, err := w.oldLogFiles()
	if err != nil {
		return err
	}
	var remove []logInfo
	if w.maxBackups > 0 && w.maxBackups < len(files) {
		preserved := make(map[string]bool)
		for _, f := range files {
			fn := f.Name()
			preserved[fn] = true

			if len(preserved) > w.maxBackups {
				remove = append(remove, f)
			}
		}
	}
	for _, f := range remove {
		errRemove := os.Remove(filepath.Join(w.dir(), f.Name()))
		if err == nil && errRemove != nil {
			err = errRemove
		}
	}
	return err
}
