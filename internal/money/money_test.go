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
		{name: "indian grouped digits from the browser", input: "1,00,000", want: 10000000},
		{name: "rupee symbol grouping and paise", input: "₹1,00,000.50", want: 10000050},
		{name: "surrounding whitespace", input: " 1,00,000 ", want: 10000000},
		{name: "space after rupee symbol", input: "₹ 1,00,000", want: 10000000},
		{name: "empty", input: "  ", wantErr: true},
		{name: "non numeric", input: "not-money", wantErr: true},
		{name: "letters", input: "abc", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
		{name: "infinity", input: "Inf", wantErr: true},
		{name: "not a number", input: "NaN", wantErr: true},
		// F-B-01: an amount whose paise overflow int64 must be refused, never
		// silently saturated to MaxInt64 (₹92,23,37,20,36,85,47,758.07).
		{name: "overflow scientific 1e300", input: "1e300", wantErr: true},
		{name: "overflow 9e18 rupees", input: "9e18", wantErr: true},
		{name: "overflow 1e19 rupees", input: "1e19", wantErr: true},
		{name: "overflow plain digits", input: "100000000000000000", wantErr: true},
		{name: "overflow just past the paise boundary", input: "92233720368547758.07", wantErr: true},
		{name: "very large but representable", input: "10000000000000000", want: 1000000000000000000},
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

func TestInWords(t *testing.T) {
	tests := []struct {
		name  string
		paise int64
		want  string
	}{
		{name: "zero", paise: 0, want: "Zero"},
		{name: "one rupee", paise: 100, want: "One"},
		{name: "paise are dropped", paise: 199, want: "One"},
		{name: "less than a rupee is zero", paise: 99, want: "Zero"},
		{name: "teens", paise: 1500, want: "Fifteen"},
		{name: "tens", paise: 9000, want: "Ninety"},
		{name: "tens with unit", paise: 4200, want: "Forty two"},
		{name: "hundreds", paise: 11500, want: "One hundred fifteen"},
		{name: "round hundred", paise: 30000, want: "Three hundred"},
		{name: "thousand", paise: 100000, want: "One thousand"},
		{name: "lakh", paise: 10000000, want: "One lakh"},
		{name: "crore", paise: 1234567800, want: "One crore twenty three lakh forty five thousand six hundred seventy eight"},
		{name: "hundred crore", paise: 100000000000, want: "One hundred crore"},
		{name: "skips empty groups", paise: 1000000100, want: "One crore one"},
		{name: "negative", paise: -100, want: "Minus one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InWords(tt.paise); got != tt.want {
				t.Fatalf("InWords(%d) = %q, want %q", tt.paise, got, tt.want)
			}
		})
	}
}

func FuzzInWordsNeverPanics(f *testing.F) {
	for _, seed := range []int64{0, 100, 199, -100, 10000000, 1234567800, 100000000000} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, paise int64) {
		if got := InWords(paise); got == "" {
			t.Fatalf("InWords(%d) = empty string", paise)
		}
	})
}

func TestPercentZeroBudget(t *testing.T) {
	if got := Percent(5000, 0); got != "N/A" {
		t.Fatalf("Percent with zero budget = %q, want N/A", got)
	}
	if got := Percent(2500, 10000); got != "25.0%" {
		t.Fatalf("Percent = %q, want 25.0%%", got)
	}
}

func FuzzParsePaiseNeverPanics(f *testing.F) {
	for _, seed := range []string{"1", "₹12,34,567.89", "0", "-1", "NaN", "1e309", "%_"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParsePaise(input)
	})
}

func FuzzFormatParsePositiveRoundTrip(f *testing.F) {
	for _, seed := range []int64{1, 105, 9999999, 123456789} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, paise int64) {
		if paise <= 0 || paise > 9_000_000_000_000 {
			t.Skip()
		}
		got, err := ParsePaise(FormatPaise(paise))
		if err != nil {
			t.Fatalf("round-trip parse failed: %v", err)
		}
		if got != paise {
			t.Fatalf("round trip = %d, want %d", got, paise)
		}
	})
}
