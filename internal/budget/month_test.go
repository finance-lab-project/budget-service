package budget

import (
	"errors"
	"fmt"
	"testing"
)

func TestValidateMonth(t *testing.T) {
	valid := []string{"0001-01", "9999-12", "1900-02", "2026-02", "2100-02"}
	for month := 1; month <= 12; month++ {
		valid = append(valid, fmt.Sprintf("2026-%02d", month))
	}
	for _, month := range valid {
		t.Run(month, func(t *testing.T) {
			if err := ValidateMonth(month); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateMonthRejectsInvalidValues(t *testing.T) {
	invalid := []string{
		"", "0000-01", "10000-01", "2026-00", "2026-13", "2026-99",
		"2026-2", "026-02", "2026-002", "2026-02-01", "202602", "2026/02",
		"-001-01", "+026-01", "202a-01", "2026-ab", "2026-1a",
		" 2026-02", "2026-02 ", "\t2026-02", "2026-02\n", "2026-02\r\n",
		"2026-02x", "2026-02\x00", "２０２６-02", "٢٠٢٦-02", "2026-０２", "2026-٠٢",
	}
	for _, month := range invalid {
		t.Run(month, func(t *testing.T) {
			if err := ValidateMonth(month); !errors.Is(err, ErrInvalidMonth) {
				t.Fatalf("error = %v, want ErrInvalidMonth", err)
			}
		})
	}
}
