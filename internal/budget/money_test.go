package budget

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMoneyRoundTrip(t *testing.T) {
	amounts := []string{
		"0", "10", "0.1", "10.01", "0.000001", "12.345678",
		"999999999999999999", "999999999999999999.999999",
	}
	for _, currency := range []Currency{RUB, USD, EUR} {
		for _, amount := range amounts {
			t.Run(string(currency)+"/"+amount, func(t *testing.T) {
				money, err := ParseMoney(amount, currency)
				if err != nil {
					t.Fatal(err)
				}
				assertMoney(t, money, amount, currency)
			})
		}
	}
}

func TestParseMoneyRejectsInvalidAmounts(t *testing.T) {
	amounts := []string{
		"", ".5", "01", "00", "01.1", "10.00", "0.000000", "1.230",
		"1e3", "1E3", "+1", "-1", "-0", "1.", "1,25", "1.2.3",
		" ", " 1", "1 ", "\t1", "1\n", "1\r\n", "1 2", "1\x00",
		"１", "١", "1.١", "NaN", "Infinity",
		"0.0000001", "1.1234567", "1000000000000000000",
		strings.Repeat("9", 1000),
	}
	for _, amount := range amounts {
		t.Run(amount, func(t *testing.T) {
			money, err := ParseMoney(amount, RUB)
			if !errors.Is(err, ErrInvalidAmount) {
				t.Fatalf("error = %v, want ErrInvalidAmount", err)
			}
			if money != (Money{}) {
				t.Fatalf("invalid parse returned Money: %#v", money)
			}
		})
	}
}

func TestCurrencyValidation(t *testing.T) {
	for _, currency := range []Currency{RUB, USD, EUR} {
		if err := currency.Validate(); err != nil {
			t.Fatalf("%q: %v", currency, err)
		}
	}
	for _, currency := range []Currency{"", "rub", "Usd", "eur", " RUB", "RUB ", "USD\n", "GBP", "BTC", "ＥＵＲ"} {
		t.Run(string(currency), func(t *testing.T) {
			if err := currency.Validate(); !errors.Is(err, ErrUnsupportedCurrency) {
				t.Fatalf("validation error = %v", err)
			}
			money, err := ParseMoney("0", currency)
			if !errors.Is(err, ErrUnsupportedCurrency) || money != (Money{}) {
				t.Fatalf("parse = %#v, %v", money, err)
			}
		})
	}
}

func TestUninitializedMoneyIsInvalid(t *testing.T) {
	var money Money
	if amount, err := money.Amount(); amount != "" || !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("Amount = %q, %v", amount, err)
	}
	if money.Currency() != "" {
		t.Fatal("uninitialized Money has a default currency")
	}
}

func TestMoneyStorageAndFormattingPreservation(t *testing.T) {
	money := mustMoney(t, "12.34", RUB)
	if got := money.units.String(); got != "12340000" {
		t.Fatalf("millionths = %s, want 12340000", got)
	}
	copied := *money
	for range 3 {
		assertMoney(t, copied, "12.34", RUB)
		assertMoney(t, *money, "12.34", RUB)
	}
	if got := money.units.String(); got != "12340000" {
		t.Fatalf("formatting mutated storage: %s", got)
	}
}

func mustMoney(t *testing.T, amount string, currency Currency) *Money {
	t.Helper()
	money, err := ParseMoney(amount, currency)
	if err != nil {
		t.Fatal(err)
	}
	return &money
}

func assertMoney(t *testing.T, money Money, want string, currency Currency) {
	t.Helper()
	got, err := money.Amount()
	if err != nil || got != want || money.Currency() != currency {
		t.Fatalf("Money = (%q, %q, %v), want (%q, %q, nil)", got, money.Currency(), err, want, currency)
	}
}
