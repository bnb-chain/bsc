package minerconfig

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBuilderConfig_GetBuilderName(t *testing.T) {
	tests := []struct {
		name     string
		config   BuilderConfig
		expected string
	}{
		{
			name: "with explicit name",
			config: BuilderConfig{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				URL:     "https://builder1.example.com",
				Name:    "test-builder-1",
			},
			expected: "test-builder-1",
		},
		{
			name: "extract from URL with subdomain",
			config: BuilderConfig{
				Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
				URL:     "https://tokyo.builder.testdomain.io",
			},
			expected: "testdomain",
		},
		{
			name: "extract from URL without subdomain",
			config: BuilderConfig{
				Address: common.HexToAddress("0x3333333333333333333333333333333333333333"),
				URL:     "https://example.com",
			},
			expected: "example",
		},
		{
			name: "extract from URL with port",
			config: BuilderConfig{
				Address: common.HexToAddress("0x4444444444444444444444444444444444444444"),
				URL:     "https://builder.example.com:8080",
			},
			expected: "example",
		},
		{
			name: "extract from URL with multiple subdomains",
			config: BuilderConfig{
				Address: common.HexToAddress("0x5555555555555555555555555555555555555555"),
				URL:     "https://api.builder.example.com",
			},
			expected: "example",
		},
		{
			name: "no URL, fallback to address",
			config: BuilderConfig{
				Address: common.HexToAddress("0x6666666666666666666666666666666666666666"),
			},
			expected: "0x6666666666666666666666666666666666666666",
		},
		{
			name: "empty URL, fallback to address",
			config: BuilderConfig{
				Address: common.HexToAddress("0x7777777777777777777777777777777777777777"),
				URL:     "",
			},
			expected: "0x7777777777777777777777777777777777777777",
		},
		{
			name: "URL with invalid format",
			config: BuilderConfig{
				Address: common.HexToAddress("0x8888888888888888888888888888888888888888"),
				URL:     "invalid-url",
			},
			expected: "invalid-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetBuilderName()
			if result != tt.expected {
				t.Errorf("GetBuilderName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMevConfig_GetBuilderName(t *testing.T) {
	config := &MevConfig{
		Builders: []BuilderConfig{
			{
				Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
				URL:     "https://builder1.example.com",
				Name:    "test-builder-1",
			},
			{
				Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
				URL:     "https://builder2.example.com",
				Name:    "test-builder-1",
			},
			{
				Address: common.HexToAddress("0x3333333333333333333333333333333333333333"),
				URL:     "https://tokyo.builder.testdomain.io",
			},
			{
				Address: common.HexToAddress("0x4444444444444444444444444444444444444444"),
				URL:     "https://dublin.builder.testdomain.io",
			},
		},
	}

	tests := []struct {
		name     string
		builder  common.Address
		expected string
	}{
		{
			name:     "builder with explicit name",
			builder:  common.HexToAddress("0x1111111111111111111111111111111111111111"),
			expected: "test-builder-1",
		},
		{
			name:     "builder with explicit name (second)",
			builder:  common.HexToAddress("0x2222222222222222222222222222222222222222"),
			expected: "test-builder-1",
		},
		{
			name:     "builder with auto-extracted name",
			builder:  common.HexToAddress("0x3333333333333333333333333333333333333333"),
			expected: "testdomain",
		},
		{
			name:     "builder with auto-extracted name (second)",
			builder:  common.HexToAddress("0x4444444444444444444444444444444444444444"),
			expected: "testdomain",
		},
		{
			name:     "unknown builder",
			builder:  common.HexToAddress("0x9999999999999999999999999999999999999999"),
			expected: "0x9999999999999999999999999999999999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.GetBuilderName(tt.builder)
			if result != tt.expected {
				t.Errorf("GetBuilderName(%v) = %v, want %v", tt.builder.Hex(), result, tt.expected)
			}
		})
	}
}

func TestExtractDomainSuffix(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "standard subdomain",
			url:      "https://tokyo.builder.testdomain.io",
			expected: "testdomain",
		},
		{
			name:     "multiple subdomains",
			url:      "https://api.builder.example.com",
			expected: "example",
		},
		{
			name:     "simple domain",
			url:      "https://example.com",
			expected: "example",
		},
		{
			name:     "with port",
			url:      "https://builder.example.com:8080",
			expected: "example",
		},
		{
			name:     "with path",
			url:      "https://tokyo.builder.testdomain.io/api/v1",
			expected: "testdomain",
		},
		{
			name:     "with query parameters",
			url:      "https://tokyo.builder.testdomain.io?param=value",
			expected: "testdomain",
		},
		{
			name:     "with fragment",
			url:      "https://tokyo.builder.testdomain.io#section",
			expected: "testdomain",
		},
		{
			name:     "invalid URL",
			url:      "invalid-url",
			expected: "invalid-url",
		},
		{
			name:     "empty URL",
			url:      "",
			expected: "",
		},
		{
			name:     "test domain 1",
			url:      "https://bsc-builder-dublin.testdomain.com",
			expected: "testdomain",
		},
		{
			name:     "test domain 2",
			url:      "https://rpc.bsc.testbuilder.xyz",
			expected: "testbuilder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDomainSuffix(tt.url)
			if result != tt.expected {
				t.Errorf("extractDomainSuffix(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestApplyDefaultMinerConfig(t *testing.T) {
	// Test with nil config
	ApplyDefaultMinerConfig(nil)

	// Test with empty config
	config := &Config{}
	ApplyDefaultMinerConfig(config)

	// Verify default values are set
	if config.Mev.MaxBidsPerBuilder == nil {
		t.Error("MaxBidsPerBuilder should be set to default value")
	} else if *config.Mev.MaxBidsPerBuilder != defaultMaxBidsPerBuilder {
		t.Errorf("MaxBidsPerBuilder = %d, want %d", *config.Mev.MaxBidsPerBuilder, defaultMaxBidsPerBuilder)
	}

	if config.Mev.Enabled == nil {
		t.Error("Enabled should be set to default value")
	} else if *config.Mev.Enabled != defaultMevEnabled {
		t.Errorf("Enabled = %v, want %v", *config.Mev.Enabled, defaultMevEnabled)
	}
}

func TestDefaultConfig(t *testing.T) {
	// Test that default config is properly initialized
	if DefaultConfig.Mev.MaxBidsPerBuilder == nil {
		t.Error("DefaultConfig.Mev.MaxBidsPerBuilder should not be nil")
	}

	if DefaultConfig.Mev.Enabled == nil {
		t.Error("DefaultConfig.Mev.Enabled should not be nil")
	}

	if DefaultConfig.Mev.GreedyMergeTx == nil {
		t.Error("DefaultConfig.Mev.GreedyMergeTx should not be nil")
	}
}
