package pool

import (
	"encoding/json"
	"os"

	"github.com/ethereum/go-ethereum/common"
)

// LPConfigEntry represents a single liquidity pool configuration entry
type LPConfigEntry struct {
	Address        string `json:"address"`
	Type           string `json:"type"`
	Token0Symbol   string `json:"token0_symbol"`
	Token1Symbol   string `json:"token1_symbol"`
	Token0Decimals uint8  `json:"token0_decimals"`
	Token1Decimals uint8  `json:"token1_decimals"`
	Fee            uint32 `json:"fee,omitempty"`
}

// LPConfigFile represents the structure of the LP configuration file
type LPConfigFile struct {
	Pools []LPConfigEntry `json:"pools"`
}

// LoadLPConfig loads liquidity pool configurations from a JSON file
func LoadLPConfig(filename string) (*LPConfigFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config LPConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// ToLPConfig converts an LPConfigEntry to an LPConfig
func (entry *LPConfigEntry) ToLPConfig() (LPConfig, error) {
	address := common.HexToAddress(entry.Address)
	lpType := LPType(entry.Type)

	return LPConfig{
		Address:        address,
		Type:           lpType,
		Token0Symbol:   entry.Token0Symbol,
		Token1Symbol:   entry.Token1Symbol,
		Token0Decimals: entry.Token0Decimals,
		Token1Decimals: entry.Token1Decimals,
		Fee:            entry.Fee,
	}, nil
}
