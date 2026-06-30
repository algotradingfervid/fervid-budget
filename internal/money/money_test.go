package money

import "testing"

func TestParsePaise(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "plain rupees", input: "1234.56", want: 123456},
		{name: "rupee symbol and commas", input: " ₹12,34,567.89 ", want: 123456789},
		{name: "rounds to nearest paise", input: "10.235", want: 1024},
		{name: "empty", input: "  ", wantErr: true},
		{name: "non numeric", input: "not-money", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePaise(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePaise(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePaise(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePaise(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatPaise(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{name: "zero", input: 0, want: "₹0.00"},
		{name: "paise padded", input: 105, want: "₹1.05"},
		{name: "indian grouping", input: 123456789, want: "₹12,34,567.89"},
		{name: "negative", input: -123456, want: "-₹1,234.56"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatPaise(tt.input); got != tt.want {
				t.Fatalf("FormatPaise(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatShort(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{name: "below lakh uses full format", input: 9999999, want: "₹99,999.99"},
		{name: "lakh", input: 12500000, want: "₹1.25 L"},
		{name: "crore", input: 25000000000, want: "₹25.00 Cr"},
		{name: "negative lakh", input: -12500000, want: "-₹1.25 L"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatShort(tt.input); got != tt.want {
				t.Fatalf("FormatShort(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPercentZeroBudget(t *testing.T) {
	if got := Percent(5000, 0); got != "N/A" {
		t.Fatalf("Percent with zero budget = %q, want N/A", got)
	}
	if got := Percent(2500, 10000); got != "25.0%" {
		t.Fatalf("Percent = %q, want 25.0%%", got)
	}
}
