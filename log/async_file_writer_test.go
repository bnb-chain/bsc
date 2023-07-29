package log

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWriterHourly(t *testing.T) {
	w := NewAsyncFileWriter("./hello.log", 100, 1)
	w.Start()
	w.Write([]byte("hello\n"))
	w.Write([]byte("world\n"))
	w.Stop()
	files, _ := os.ReadDir("./")
	for _, f := range files {
		fn := f.Name()
		if strings.HasPrefix(fn, "hello") {
			t.Log(fn)
			content, _ := os.ReadFile(fn)
			t.Log(content)
			os.Remove(fn)
		}
	}
}

func TestGetNextRotationHour(t *testing.T) {
	tcs := []struct {
		now          time.Time
		delta        int
		expectedHour int
	}{
		{
			now:          time.Date(1980, 1, 6, 15, 34, 0, 0, time.UTC),
			delta:        3,
			expectedHour: 18,
		},
		{
			now:          time.Date(1980, 1, 6, 23, 59, 0, 0, time.UTC),
			delta:        1,
			expectedHour: 0,
		},
		{
			now:          time.Date(1980, 1, 6, 22, 15, 0, 0, time.UTC),
			delta:        2,
			expectedHour: 0,
		},
		{
			now:          time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC),
			delta:        1,
			expectedHour: 1,
		},
	}

	test := func(now time.Time, delta, expectedHour int) func(*testing.T) {
		return func(t *testing.T) {
			got := getNextRotationHour(now, delta)
			if got != expectedHour {
				t.Fatalf("Expected %d, found: %d\n", expectedHour, got)
			}
		}
	}

	for i, tc := range tcs {
		t.Run("TestGetNextRotationHour_"+strconv.Itoa(i), test(tc.now, tc.delta, tc.expectedHour))
	}
}
