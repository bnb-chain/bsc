package rawdb

import "testing"

func TestValidateStateScheme(t *testing.T) {
	tests := []struct {
		name       string
		arg        string
		wantResult bool
	}{
		{
			name:       "hash scheme",
			arg:        HashScheme,
			wantResult: true,
		},
		{
			name:       "path scheme",
			arg:        PathScheme,
			wantResult: true,
		},
		{
			name:       "invalid scheme",
			arg:        "mockScheme",
			wantResult: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateStateScheme(tt.arg); got != tt.wantResult {
				t.Errorf("ValidateStateScheme() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
