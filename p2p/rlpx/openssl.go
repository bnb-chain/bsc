package rlpx

import (
	"fmt"
	"os"
	"strings"

	mopenssl "github.com/microsoft/go-crypto-openssl/openssl"
)

var OpenSsl = false
var flagOpenssl = "openssl"

func init() {
	for _, arg := range os.Args {
		flag := strings.TrimLeft(arg, "-")

		if !OpenSsl && flag == flagOpenssl {
			//log.Info is not yet initialized so use fmt instead.
			fmt.Println("openssl enabled")
			OpenSsl = true
			mopenssl.Init()
		}

	}
}