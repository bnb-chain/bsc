package main

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/log"
)

func TestSlashIndicatorFinalityEvidenceEncoding(t *testing.T) {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	evidence := `{"VoteA":{"SrcNum":1234,"SrcHash":"36068b819f244d27b5411d975f9ffd6d18c6084b50fb5595104ffd9de561a9f8","TarNum":1234,"TarHash":"36068b819f244d27b5411d975f9ffd6d18c6084b50fb5595104ffd9de561a9f8","Sig":"893682ebf26440a06daaff5695945ee2012146268f800c217bad9906ac64dc46996cd435e3e829529aa0445b52530070893682ebf26440a06daaff5695945ee2012146268f800c217bad9906ac64dc46996cd435e3e829529aa0445b52530070"},"VoteB":{"SrcNum":1234,"SrcHash":"36068b819f244d27b5411d975f9ffd6d18c6084b50fb5595104ffd9de561a9f8","TarNum":1234,"TarHash":"36068b819f244d27b5411d975f9ffd6d18c6084b50fb5595104ffd9de561a9f8","Sig":"893682ebf26440a06daaff5695945ee2012146268f800c217bad9906ac64dc46996cd435e3e829529aa0445b52530070893682ebf26440a06daaff5695945ee2012146268f800c217bad9906ac64dc46996cd435e3e829529aa0445b52530070"},"VoteAddr":"893682ebf26440a06daaff5695945ee2012146268f800c217bad9906ac64dc46996cd435e3e829529aa0445b52530070"}`

	slashIndicatorFinalityEvidence := &SlashIndicatorFinalityEvidence{}
	if err := slashIndicatorFinalityEvidence.UnmarshalJSON([]byte(evidence)); err != nil {
		log.Crit("SlashIndicatorFinalityEvidence UnmarshalJSON failed")
	}
	if output, err := slashIndicatorFinalityEvidence.MarshalJSON(); err != nil {
		log.Crit("SlashIndicatorFinalityEvidence MarshalJSON failed")
	} else if string(output) != evidence {
		log.Crit("SlashIndicatorFinalityEvidence UnmarshalJSON MarshalJSON mismatch", "output", string(output), "evidence", evidence)
	}
}
