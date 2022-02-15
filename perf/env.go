package perf

import (
	"os"
	"strconv"
)

func getEnvBool(key string) (bool, error) {
	s := os.Getenv(key)
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, err
	}
	return v, nil
}
