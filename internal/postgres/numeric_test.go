package postgres

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestNumericToMoneyNull(t *testing.T) {
	money, err := numericToMoney(pgtype.Numeric{}, budget.RUB)
	if money != nil || err != nil {
		t.Fatalf("SQL NULL = (%v, %v), want (nil, nil)", money, err)
	}
}

func TestNumericToMoneyExactValues(t *testing.T) {
	for _, currency := range []budget.Currency{budget.RUB, budget.USD, budget.EUR} {
		for _, tt := range []struct {
			name        string
			coefficient string
			exponent    int32
			want        string
		}{
			{"planned zero", "0", 0, "0"},
			{"padded zero", "0", -7, "0"},
			{"zero minimum exponent", "0", math.MinInt32, "0"},
			{"zero maximum exponent", "0", math.MaxInt32, "0"},
			{"integer", "1", 0, "1"},
			{"positive exponent", "123", 2, "12300"},
			{"one millionth", "1", -6, "0.000001"},
			{"padded millionth", "10", -7, "0.000001"},
			{"padded integer", "10000000", -7, "1"},
			{"padded decimal", "123400000", -7, "12.34"},
			{"six fractional digits", "12345678", -6, "12.345678"},
			{"integer beyond float precision", "9007199254740993", 0, "9007199254740993"},
			{"units beyond float precision", "9007199254740993", -6, "9007199254.740993"},
			{"maximum integer", "999999999999999999", 0, "999999999999999999"},
			{"maximum planned amount", "999999999999999999999999", -6, "999999999999999999.999999"},
			{"padded maximum", "9999999999999999999999990", -7, "999999999999999999.999999"},
			{"long zero padding", "1" + strings.Repeat("0", 1000), -1000, "1"},
		} {
			t.Run(string(currency)+"/"+tt.name, func(t *testing.T) {
				value := testNumeric(t, tt.coefficient, tt.exponent)
				before := new(big.Int).Set(value.Int)
				money, err := numericToMoney(value, currency)
				if err != nil {
					t.Fatal(err)
				}
				assertDecodedMoney(t, money, tt.want, currency)
				if value.Int.Cmp(before) != 0 {
					t.Fatal("conversion changed the source integer")
				}
			})
		}
	}
}

func TestNumericToMoneyRejectsInvalidValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value pgtype.Numeric
	}{
		{"missing coefficient", pgtype.Numeric{Valid: true}},
		{"NaN", pgtype.Numeric{NaN: true, Valid: true}},
		{"positive infinity", pgtype.Numeric{InfinityModifier: pgtype.Infinity, Valid: true}},
		{"negative infinity", pgtype.Numeric{InfinityModifier: pgtype.NegativeInfinity, Valid: true}},
		{"invalid infinity modifier", pgtype.Numeric{Int: big.NewInt(1), InfinityModifier: 2, Valid: true}},
		{"negative integer", testNumeric(t, "-1", 0)},
		{"negative millionth", testNumeric(t, "-1", -6)},
		{"negative sub-millionth", testNumeric(t, "-1", -7)},
		{"sub-millionth", testNumeric(t, "1", -7)},
		{"nonzero discarded digit", testNumeric(t, "10000001", -7)},
		{"excess fractional precision", testNumeric(t, "12345678", -7)},
		{"integer overflow", testNumeric(t, "1000000000000000000", 0)},
		{"scaled overflow", testNumeric(t, "1", 18)},
		{"one millionth above maximum", testNumeric(t, "1000000000000000000000000", -6)},
		{"maximum exponent", testNumeric(t, "1", math.MaxInt32)},
		{"minimum exponent", testNumeric(t, "1", math.MinInt32)},
		{"oversized coefficient", testNumeric(t, strings.Repeat("9", 1000), 0)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var before *big.Int
			if tt.value.Int != nil {
				before = new(big.Int).Set(tt.value.Int)
			}
			money, err := numericToMoney(tt.value, budget.RUB)
			if money != nil || !errors.Is(err, ErrInvalidStoredData) || !errors.Is(err, budget.ErrInvalidAmount) {
				t.Fatalf("got (%v, %v), want no result and wrapped invalid stored amount", money, err)
			}
			if errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrDatabase) {
				t.Fatalf("stored-data failure has the wrong error category: %v", err)
			}
			if before != nil && tt.value.Int.Cmp(before) != 0 {
				t.Fatal("rejected conversion changed the source integer")
			}
		})
	}
}

func TestNumericToMoneyRejectsUnsupportedCurrency(t *testing.T) {
	for _, currency := range []budget.Currency{"", "rub", "GBP"} {
		for _, coefficient := range []string{"0", "1"} {
			t.Run(string(currency)+"/"+coefficient, func(t *testing.T) {
				money, err := numericToMoney(testNumeric(t, coefficient, 0), currency)
				if money != nil || !errors.Is(err, ErrInvalidStoredData) || !errors.Is(err, budget.ErrUnsupportedCurrency) {
					t.Fatalf("got (%v, %v), want no result and wrapped unsupported currency", money, err)
				}
			})
		}
	}
}

func TestNumericToMoneyPreservesIndependentResults(t *testing.T) {
	for _, tt := range []struct {
		coefficient string
		exponent    int32
		want        string
	}{
		{"123", 2, "12300"},
		{"123400000", -7, "12.34"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			value := testNumeric(t, tt.coefficient, tt.exponent)
			first, err := numericToMoney(value, budget.USD)
			if err != nil {
				t.Fatal(err)
			}
			second, err := numericToMoney(value, budget.EUR)
			if err != nil {
				t.Fatal(err)
			}
			value.Int.SetInt64(0)
			assertDecodedMoney(t, first, tt.want, budget.USD)
			assertDecodedMoney(t, second, tt.want, budget.EUR)
		})
	}
}

func TestNumericToMoneyKeepsIncomingParserStrict(t *testing.T) {
	for _, tt := range []struct{ stored, want string }{
		{"1.0000000", "1"},
		{"0.0000010", "0.000001"},
		{"12.3400", "12.34"},
	} {
		t.Run(tt.stored, func(t *testing.T) {
			if _, err := budget.ParseMoney(tt.stored, budget.RUB); !errors.Is(err, budget.ErrInvalidAmount) {
				t.Fatalf("incoming parser accepted noncanonical amount: %v", err)
			}
			var value pgtype.Numeric
			if err := value.Scan(tt.stored); err != nil {
				t.Fatal(err)
			}
			money, err := numericToMoney(value, budget.RUB)
			if err != nil {
				t.Fatal(err)
			}
			assertDecodedMoney(t, money, tt.want, budget.RUB)
		})
	}
}

func testNumeric(t *testing.T, coefficient string, exponent int32) pgtype.Numeric {
	t.Helper()
	integer, ok := new(big.Int).SetString(coefficient, 10)
	if !ok {
		t.Fatal("invalid test coefficient")
	}
	return pgtype.Numeric{Int: integer, Exp: exponent, Valid: true}
}

func assertDecodedMoney(t *testing.T, money *budget.Money, want string, currency budget.Currency) {
	t.Helper()
	if money == nil {
		t.Fatal("planned amount became unplanned")
	}
	amount, err := money.Amount()
	if err != nil || amount != want || money.Currency() != currency {
		t.Fatalf("Money = (%q, %q, %v), want (%q, %q, nil)", amount, money.Currency(), err, want, currency)
	}
}
