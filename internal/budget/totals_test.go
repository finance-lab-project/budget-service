package budget

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
)

const maximumAmount = "999999999999999999.999999"

// Keep the contract list independent from the production catalog.
var categoryIDs = []string{
	"groceries", "restaurants", "utilities", "transport", "household",
	"health", "personal-care", "clothing", "subscriptions", "education",
	"tech", "culture", "entertainment", "gifts", "travel",
}

func TestCalculateTotalsReferenceFixtures(t *testing.T) {
	tests := []struct {
		name        string
		income      string
		spending    map[string]string
		investments string
		allMaximum  bool
		sequential  bool
		want        [4]string
	}{
		{name: "unplanned", want: [4]string{"0", "0", "0", "0"}},
		{name: "decimal cancellation", income: "0.3",
			spending: map[string]string{"groceries": "0.1", "restaurants": "0.2"},
			want:     [4]string{"0.3", "0.3", "0", "0"}},
		{name: "negative remaining", income: "0.1",
			spending: map[string]string{"groceries": "0.2"}, investments: "0.000001",
			want: [4]string{"0.1", "0.2", "0.000001", "-0.100001"}},
		{name: "all categories contribute", income: "200", sequential: true, investments: "3",
			want: [4]string{"200", "120", "3", "77"}},
		{name: "maximum spending", allMaximum: true,
			want: [4]string{"0", "14999999999999999999.999985", "0", "-14999999999999999999.999985"}},
		{name: "maximum outflow", income: "0", allMaximum: true, investments: maximumAmount,
			want: [4]string{"0", "14999999999999999999.999985", maximumAmount, "-15999999999999999999.999984"}},
		{name: "negative millionth", investments: "0.000001",
			want: [4]string{"0", "0", "0.000001", "-0.000001"}},
		{name: "integer formatting", income: "2", spending: map[string]string{"groceries": "0.5", "restaurants": "0.5"},
			investments: "3", want: [4]string{"2", "1", "3", "-2"}},
	}
	for _, currency := range []Currency{RUB, USD, EUR} {
		for _, tt := range tests {
			t.Run(string(currency)+"/"+tt.name, func(t *testing.T) {
				input := unplanned(currency)
				if tt.income != "" {
					input.Income = mustMoney(t, tt.income, currency)
				}
				if tt.investments != "" {
					input.Investments = mustMoney(t, tt.investments, currency)
				}
				for i, id := range categoryIDs {
					amount := tt.spending[id]
					if tt.allMaximum {
						amount = maximumAmount
					} else if tt.sequential {
						amount = strconv.Itoa(i + 1)
					}
					if amount != "" {
						input.Spending[id] = mustMoney(t, amount, currency)
					}
				}
				before := snapshot(t, input)
				for range 3 {
					totals, err := CalculateTotals(input)
					if err != nil {
						t.Fatal(err)
					}
					assertTotals(t, totals, tt.want, currency)
					if !reflect.DeepEqual(snapshot(t, input), before) {
						t.Fatal("calculation changed input amounts or absence")
					}
				}
			})
		}
	}
}

func TestUnplannedAndExplicitZero(t *testing.T) {
	input := unplanned(RUB)
	empty, err := CalculateTotals(input)
	if err != nil {
		t.Fatal(err)
	}
	zero := mustMoney(t, "0", RUB)
	input.Income, input.Investments = zero, zero
	input.Spending["groceries"] = zero
	got, err := CalculateTotals(input)
	if err != nil {
		t.Fatal(err)
	}
	want := [4]string{"0", "0", "0", "0"}
	assertTotals(t, empty, want, RUB)
	assertTotals(t, got, want, RUB)
	if input.Income != zero || input.Investments != zero ||
		input.Spending["groceries"] != zero || input.Spending["restaurants"] != nil {
		t.Fatal("calculation rewrote absence or planned zero")
	}
}

func TestCategoryCatalogAndReordering(t *testing.T) {
	ids := SpendingCategoryIDs()
	if !reflect.DeepEqual(ids[:], categoryIDs) {
		t.Fatalf("category catalog = %v", ids)
	}
	ids[0] = "changed"
	if SpendingCategoryIDs()[0] != "groceries" {
		t.Fatal("caller changed domain catalog")
	}

	for _, reverse := range []bool{false, true} {
		input := unplanned(EUR)
		input.Income = mustMoney(t, "200", EUR)
		input.Investments = mustMoney(t, "3", EUR)
		input.Spending = make(map[string]*Money)
		for i := range categoryIDs {
			index := i
			if reverse {
				index = len(categoryIDs) - 1 - i
			}
			input.Spending[categoryIDs[index]] = mustMoney(t, strconv.Itoa(index+1), EUR)
		}
		before := snapshot(t, input)
		totals, err := CalculateTotals(input)
		if err != nil {
			t.Fatal(err)
		}
		assertTotals(t, totals, [4]string{"200", "120", "3", "77"}, EUR)
		if !reflect.DeepEqual(snapshot(t, input), before) {
			t.Fatal("calculation changed caller-owned categories")
		}
	}
}

func TestCalculateTotalsRejectsInvalidInput(t *testing.T) {
	type testCase struct {
		name   string
		change func(*MonthlyInput)
		want   error
	}
	tests := []testCase{
		{"missing currency", func(in *MonthlyInput) { in.Currency = "" }, ErrUnsupportedCurrency},
		{"unsupported currency", func(in *MonthlyInput) { in.Currency = "GBP" }, ErrUnsupportedCurrency},
		{"lowercase currency", func(in *MonthlyInput) { in.Currency = "rub" }, ErrUnsupportedCurrency},
		{"padded currency", func(in *MonthlyInput) { in.Currency = "RUB " }, ErrUnsupportedCurrency},
		{"nil categories", func(in *MonthlyInput) { in.Spending = nil }, ErrInvalidCategories},
		{"missing category", func(in *MonthlyInput) { delete(in.Spending, "groceries") }, ErrInvalidCategories},
		{"extra category", func(in *MonthlyInput) { in.Spending["unknown"] = nil }, ErrInvalidCategories},
		{"unknown replacement", func(in *MonthlyInput) {
			delete(in.Spending, "groceries")
			in.Spending["unknown"] = nil
		}, ErrInvalidCategories},
	}
	for _, field := range []string{"income", "spending", "investments"} {
		for _, amount := range []string{"0", "1"} {
			tests = append(tests, testCase{field + " currency mismatch " + amount, func(in *MonthlyInput) {
				setField(in, field, mustMoney(t, amount, USD))
			}, ErrCurrencyMismatch})
		}
		tests = append(tests, testCase{field + " uninitialized Money", func(in *MonthlyInput) {
			setField(in, field, &Money{})
		}, ErrInvalidAmount})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := unplanned(RUB)
			input.Income = mustMoney(t, "100", RUB)
			input.Spending["groceries"] = mustMoney(t, "1", RUB)
			tt.change(&input)
			got, err := CalculateTotals(input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if got != (Totals{}) {
				t.Fatalf("error returned partial totals: %#v", got)
			}
		})
	}
}

func TestComputedMoneyMustMeetPlannedInputBounds(t *testing.T) {
	input := unplanned(RUB)
	for _, id := range categoryIDs {
		input.Spending[id] = mustMoney(t, maximumAmount, RUB)
	}
	totals, err := CalculateTotals(input)
	if err != nil {
		t.Fatal(err)
	}
	smallDeficit := unplanned(RUB)
	smallDeficit.Investments = mustMoney(t, "1", RUB)
	negative, err := CalculateTotals(smallDeficit)
	if err != nil {
		t.Fatal(err)
	}
	for _, computed := range []Money{totals.Spending, totals.Remaining, negative.Remaining} {
		for _, field := range []string{"income", "spending", "investments"} {
			plan := unplanned(RUB)
			setField(&plan, field, &computed)
			got, err := CalculateTotals(plan)
			if !errors.Is(err, ErrInvalidAmount) || got != (Totals{}) {
				t.Fatalf("%s accepted out-of-bounds computed Money: %#v, %v", field, got, err)
			}
		}
	}
}

func TestCalculationsPreserveSharedInputsAndEarlierResults(t *testing.T) {
	shared := mustMoney(t, "0.1", USD)
	input := unplanned(USD)
	input.Income = shared
	input.Investments = shared
	for _, id := range categoryIDs {
		input.Spending[id] = shared
	}
	before := snapshot(t, input)
	first, err := CalculateTotals(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CalculateTotals(input)
	if err != nil {
		t.Fatal(err)
	}
	want := [4]string{"0.1", "1.5", "0.1", "-1.5"}
	assertTotals(t, first, want, USD)
	assertTotals(t, second, want, USD)
	if !reflect.DeepEqual(snapshot(t, input), before) {
		t.Fatal("calculation mutated shared input Money")
	}
	for _, id := range categoryIDs {
		if input.Spending[id] != shared {
			t.Fatal("calculation replaced caller-owned category values")
		}
	}

	// Deliberately mutate private storage here to expose shallow big.Int copies.
	// Production callers cannot access it. Results must own independent storage.
	shared.units.SetInt64(1)
	assertTotals(t, first, want, USD)
	assertTotals(t, second, want, USD)
	second.Income.units.SetInt64(2)
	second.Spending.units.SetInt64(3)
	second.Investments.units.SetInt64(4)
	second.Remaining.units.SetInt64(5)
	assertTotals(t, first, want, USD)
	assertMoney(t, second.Income, "0.000002", USD)
	assertMoney(t, second.Spending, "0.000003", USD)
	assertMoney(t, second.Investments, "0.000004", USD)
	assertMoney(t, second.Remaining, "0.000005", USD)
	assertMoney(t, *shared, "0.000001", USD)
}

func unplanned(currency Currency) MonthlyInput {
	input := MonthlyInput{Currency: currency, Spending: make(map[string]*Money)}
	for _, id := range categoryIDs {
		input.Spending[id] = nil
	}
	return input
}

func setField(input *MonthlyInput, field string, money *Money) {
	switch field {
	case "income":
		input.Income = money
	case "spending":
		input.Spending["travel"] = money
	case "investments":
		input.Investments = money
	}
}

func assertTotals(t *testing.T, totals Totals, want [4]string, currency Currency) {
	t.Helper()
	for i, money := range []Money{totals.Income, totals.Spending, totals.Investments, totals.Remaining} {
		assertMoney(t, money, want[i], currency)
	}
}

func snapshot(t *testing.T, input MonthlyInput) map[string]string {
	t.Helper()
	values := map[string]*Money{"income": input.Income, "investments": input.Investments}
	for id, money := range input.Spending {
		values[id] = money
	}
	result := map[string]string{"currency": string(input.Currency)}
	for id, money := range values {
		result[id] = "<unplanned>"
		if money != nil {
			amount, err := money.Amount()
			if err != nil {
				t.Fatal(err)
			}
			result[id] = string(money.Currency()) + ":" + amount
		}
	}
	return result
}
