package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

func main() {
	var (
		hexArg  string
		fileArg string
		outArg  string
		format  string
		title   string
	)

	flag.StringVar(&hexArg, "hex", "", "contract bytecode as hex (with or without 0x prefix)")
	flag.StringVar(&fileArg, "file", "", "path to file containing contract bytecode hex")
	flag.StringVar(&outArg, "out", "", "output file path (.dot or .svg). If empty, write DOT to stdout")
	flag.StringVar(&format, "format", "", "output format: dot or svg (inferred from --out when omitted)")
	flag.StringVar(&title, "title", "", "graph title (optional)")
	flag.Parse()

	if hexArg == "" && fileArg == "" {
		usage()
		fatal(errors.New("one of --hex or --file is required"))
	}

	code, err := loadBytecode(hexArg, fileArg)
	if err != nil {
		fatal(err)
	}

	// Ensure opcode parsing is enabled
	compiler.EnableOpcodeParse()

	cfg, err := compiler.GenerateMIRCFG(common.Hash{}, code)
	if err != nil {
		fatal(fmt.Errorf("generate CFG: %w", err))
	}

	dot := buildDOT(cfg, title)

	// Determine format
	if format == "" && outArg != "" {
		ext := strings.ToLower(filepath.Ext(outArg))
		switch ext {
		case ".svg":
			format = "svg"
		case ".dot":
			format = "dot"
		default:
			format = "dot"
		}
	}
	if format == "" {
		format = "dot"
	}

	switch format {
	case "dot":
		if outArg == "" {
			os.Stdout.Write(dot)
			return
		}
		if err := os.WriteFile(outArg, dot, 0o644); err != nil {
			fatal(err)
		}
		return
	case "svg":
		// Attempt to use graphviz dot
		if _, err := exec.LookPath("dot"); err != nil {
			fatal(errors.New("dot not found in PATH; install graphviz or choose --format=dot"))
		}
		var svgOut bytes.Buffer
		cmd := exec.Command("dot", "-Tsvg")
		cmd.Stdin = bytes.NewReader(dot)
		cmd.Stdout = &svgOut
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fatal(fmt.Errorf("dot render: %w", err))
		}
		if outArg == "" {
			os.Stdout.Write(svgOut.Bytes())
			return
		}
		if err := os.WriteFile(outArg, svgOut.Bytes(), 0o644); err != nil {
			fatal(err)
		}
		return
	default:
		fatal(fmt.Errorf("unknown format %q (use dot or svg)", format))
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "mircfgdraw - generate MIR CFG as DOT/SVG\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  mircfgdraw --hex 0x... [--out graph.dot|graph.svg] [--format dot|svg] [--title title]\n")
	fmt.Fprintf(os.Stderr, "  mircfgdraw --file bytecode.hex [--out graph.dot|graph.svg] [--format dot|svg] [--title title]\n")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "mircfgdraw: %v\n", err)
	os.Exit(1)
}

func loadBytecode(hexArg, fileArg string) ([]byte, error) {
	if hexArg != "" {
		return decodeHexString(hexArg)
	}
	// Read file and concatenate non-whitespace characters
	f, err := os.Open(fileArg)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b strings.Builder
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadString('\n')
		b.WriteString(line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return decodeHexString(b.String())
}

func decodeHexString(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s)%2 == 1 { // odd length hex
		return nil, fmt.Errorf("hex string has odd length: %d", len(s))
	}
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	return data, nil
}

func buildDOT(cfg *compiler.CFG, title string) []byte {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	fmt.Fprintln(w, "digraph MIRCFG {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box, fontname=\"monospace\"];")
	if title != "" {
		fmt.Fprintf(w, "  labelloc=\"t\";\n  label=\"%s\";\n", escapeDOT(title))
	}

	blocks := cfg.GetBasicBlocks()
	indexOf := make(map[*compiler.MIRBasicBlock]int, len(blocks))
	for i, bb := range blocks {
		if bb == nil {
			continue
		}
		indexOf[bb] = i
	}
	// Nodes
	for i, bb := range blocks {
		if bb == nil {
			continue
		}
		// Determine first and last MIR op names if available
		firstOp, lastOp := "", ""
		ins := bb.Instructions()
		for _, m := range ins { // first non-nil
			if m != nil {
				firstOp = m.Op().String()
				break
			}
		}
		for j := len(ins) - 1; j >= 0; j-- { // last non-nil
			if ins[j] != nil {
				lastOp = ins[j].Op().String()
				break
			}
		}
		label := fmt.Sprintf("BB%d\\n[%d,%d) insns=%d\\nfirst:%s\\nlast:%s", i, bb.FirstPC(), bb.LastPC(), len(ins), firstOp, lastOp)
		fmt.Fprintf(w, "  n%d [label=\"%s\"];\n", i, escapeDOT(label))
	}
	// Edges
	for i, bb := range blocks {
		if bb == nil {
			continue
		}
		for _, ch := range bb.Children() {
			j, ok := indexOf[ch]
			if !ok {
				continue
			}
			fmt.Fprintf(w, "  n%d -> n%d;\n", i, j)
		}
	}
	fmt.Fprintln(w, "}")
	w.Flush()
	return buf.Bytes()
}

func escapeDOT(s string) string {
	// Keep backslash sequences (like \n) intact so Graphviz can interpret them.
	// Only escape double-quotes and convert literal newlines to \n.
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
