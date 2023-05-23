package log

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWriter(t *testing.T) {
	w := NewAsyncFileWriter("./hello.log", 100, nil)
	w.Start()
	w.Write([]byte("hello\n"))
	w.Write([]byte("world\n"))
	w.Stop()
	files, _ := ioutil.ReadDir("./")
	for _, f := range files {
		fn := f.Name()
		fmt.Println(fn)
		if strings.HasPrefix(fn, "hello") {
			t.Log(fn)
			content, _ := ioutil.ReadFile(fn)
			t.Log(content)
			os.Remove(fn)
		}
	}
}

func TestWriterNames(t *testing.T) {
	interval := 2 * time.Second
	w := NewAsyncFileWriter("./hello.log", 1000, &interval)
	w.Start()
	w.Write([]byte("hello\n"))
	// time.Sleep(3 * time.Second)
	w.Write([]byte("cruel\n"))
	// time.Sleep(3 * time.Second)
	w.Write([]byte("world\n"))
	w.Stop()
	files, _ := ioutil.ReadDir("./")
	logCounter := 0
	for _, f := range files {
		fn := f.Name()
		if strings.HasPrefix(fn, "hello") {
			if logCounter == 0 {
				logCounter++
				continue
			}

			t.Log(fn)
			fmt.Println(logCounter, " ", fn)
			secs, _ := strconv.Atoi(fn[len(fn)-2:])
			fmt.Println(secs)
			if logCounter >= 2 {
				assert.Equal(t, secs%2, 0, "Must be divisible by 2")
			}
			content, _ := ioutil.ReadFile(fn)
			t.Log(content)
			os.Remove(fn)
			logCounter++
		}
	}
}
