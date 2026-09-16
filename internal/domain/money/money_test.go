package money_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
)

func TestParseMoney(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name       string
		amount     string
		currency   string
		wantResult money.Money
		wantErr    error
	}{
		{name: "should accept when amount has full scale", amount: "25.00", currency: "BRL", wantResult: money.MustNew(2500, brl)},
		{name: "should accept when amount has no decimals", amount: "25", currency: "BRL", wantResult: money.MustNew(2500, brl)},
		{name: "should normalize scale when amount has one decimal place", amount: "25.5", currency: "BRL", wantResult: money.MustNew(2550, brl)},
		{name: "should accept when amount is zero", amount: "0.00", currency: "BRL", wantResult: money.MustNew(0, brl)},
		{name: "should accept when zero has no decimals", amount: "0", currency: "BRL", wantResult: money.MustNew(0, brl)},
		{name: "should accept when amount has only cents", amount: "0.07", currency: "BRL", wantResult: money.MustNew(7, brl)},
		{name: "should accept when amount has leading zeros", amount: "0025.00", currency: "BRL", wantResult: money.MustNew(2500, brl)},
		{name: "should accept when amount is at the upper bound", amount: "92233720368547758.07", currency: "BRL", wantResult: money.MustNew(math.MaxInt64, brl)},
		{name: "should accept when currency is not BRL", amount: "25.00", currency: "USD", wantResult: money.MustNew(2500, money.MustCurrency("USD"))},

		{name: "should return INVALID_AMOUNT when amount is empty", amount: "", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount is negative", amount: "-1.00", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount is NaN", amount: "NaN", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount is Infinity", amount: "Infinity", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount uses scientific notation", amount: "1e5", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount uses scientific notation with a point", amount: "1.5e3", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount exceeds the scale", amount: "25.123", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount has a separator without decimals", amount: "25.", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount has no integer part", amount: ".50", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount has an explicit plus sign", amount: "+25.00", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount has a leading space", amount: " 25.00", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount has two separators", amount: "25.0.0", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount is text", amount: "abc", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when amount uses a comma separator", amount: "25,00", currency: "BRL", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return AMOUNT_OVERFLOW when amount is above the upper bound", amount: "92233720368547758.08", currency: "BRL", wantErr: domain.FailureCodeAmountOverflow},

		{name: "should return INVALID_INPUT when currency is empty", amount: "1.00", currency: "", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when currency is too short", amount: "1.00", currency: "BR", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when currency is too long", amount: "1.00", currency: "BRLL", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when currency is lowercase", amount: "1.00", currency: "brl", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when currency has a digit", amount: "1.00", currency: "B1L", wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.Parse(test.amount, test.currency)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestParseSignedMoney(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name       string
		amount     string
		wantResult money.Money
		wantErr    error
	}{
		{name: "should accept when amount is negative", amount: "-25.50", wantResult: money.MustNew(-2550, brl)},
		{name: "should accept when amount is negative cents", amount: "-0.01", wantResult: money.MustNew(-1, brl)},
		{name: "should accept when amount is positive", amount: "25.50", wantResult: money.MustNew(2550, brl)},
		{name: "should normalize to zero when amount is negative zero", amount: "-0.00", wantResult: money.MustNew(0, brl)},
		{name: "should accept when amount is the negated upper bound", amount: "-92233720368547758.07", wantResult: money.MustNew(-math.MaxInt64, brl)},
		{name: "should return AMOUNT_OVERFLOW when amount is at the lower bound", amount: "-92233720368547758.08", wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_AMOUNT when amount is a lone minus sign", amount: "-", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when a negative amount exceeds the scale", amount: "-25.123", wantErr: domain.FailureCodeInvalidAmount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.ParseSigned(test.amount, "BRL")

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneyAdd(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	usd := money.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       money.Money
		wantResult money.Money
		wantErr    error
	}{
		{name: "should return the sum when both values are positive", a: money.MustNew(10000, brl), b: money.MustNew(8000, brl), wantResult: money.MustNew(18000, brl)},
		{name: "should return the sum when the operand is negative", a: money.MustNew(10000, brl), b: money.MustNew(-8000, brl), wantResult: money.MustNew(2000, brl)},
		{name: "should return the receiver when the operand is zero", a: money.MustNew(10000, brl), b: money.MustNew(0, brl), wantResult: money.MustNew(10000, brl)},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: money.MustNew(1000, brl), b: money.MustNew(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return AMOUNT_OVERFLOW when the sum is above the upper bound", a: money.MustNew(math.MaxInt64, brl), b: money.MustNew(1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return AMOUNT_OVERFLOW when the sum is below the lower bound", a: money.MustNew(math.MinInt64, brl), b: money.MustNew(-1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: money.Money{}, b: money.MustNew(1000, brl), wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the operand is uninitialized", a: money.MustNew(1000, brl), b: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.a.Add(test.b)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneySub(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	usd := money.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       money.Money
		wantResult money.Money
		wantErr    error
	}{
		{name: "should return the difference when the result is positive", a: money.MustNew(10000, brl), b: money.MustNew(8000, brl), wantResult: money.MustNew(2000, brl)},
		{name: "should return the difference when the result is negative", a: money.MustNew(8000, brl), b: money.MustNew(10000, brl), wantResult: money.MustNew(-2000, brl)},
		{name: "should return zero when both values are equal", a: money.MustNew(10000, brl), b: money.MustNew(10000, brl), wantResult: money.MustNew(0, brl)},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: money.MustNew(1000, brl), b: money.MustNew(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return AMOUNT_OVERFLOW when the difference is below the lower bound", a: money.MustNew(math.MinInt64, brl), b: money.MustNew(1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return AMOUNT_OVERFLOW when the difference is above the upper bound", a: money.MustNew(math.MaxInt64, brl), b: money.MustNew(-1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: money.Money{}, b: money.MustNew(1000, brl), wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the operand is uninitialized", a: money.MustNew(1000, brl), b: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.a.Sub(test.b)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneyNeg(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      money.Money
		wantResult money.Money
		wantErr    error
	}{
		{name: "should return a negative value when the receiver is positive", value: money.MustNew(8000, brl), wantResult: money.MustNew(-8000, brl)},
		{name: "should return a positive value when the receiver is negative", value: money.MustNew(-8000, brl), wantResult: money.MustNew(8000, brl)},
		{name: "should return zero when the receiver is zero", value: money.MustNew(0, brl), wantResult: money.MustNew(0, brl)},
		{name: "should return the negated value when the receiver is at the upper bound", value: money.MustNew(math.MaxInt64, brl), wantResult: money.MustNew(-math.MaxInt64, brl)},
		{name: "should return AMOUNT_OVERFLOW when the receiver is at the lower bound", value: money.MustNew(math.MinInt64, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", value: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.value.Neg()

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneyCmp(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	usd := money.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       money.Money
		wantResult int
		wantErr    error
	}{
		{name: "should return -1 when the receiver is smaller", a: money.MustNew(1000, brl), b: money.MustNew(2000, brl), wantResult: -1},
		{name: "should return 1 when the receiver is greater", a: money.MustNew(2000, brl), b: money.MustNew(1000, brl), wantResult: 1},
		{name: "should return 0 when both values are equal", a: money.MustNew(1000, brl), b: money.MustNew(1000, brl), wantResult: 0},
		{name: "should return -1 when the receiver is negative and the operand is positive", a: money.MustNew(-1000, brl), b: money.MustNew(1000, brl), wantResult: -1},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: money.MustNew(1000, brl), b: money.MustNew(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: money.Money{}, b: money.MustNew(1000, brl), wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.a.Cmp(test.b)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneyValidate(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name    string
		value   money.Money
		wantErr error
	}{
		{name: "should accept when the value is initialized", value: money.MustNew(1000, brl)},
		{name: "should accept when the value is initialized at zero", value: money.MustNew(0, brl)},
		{name: "should return INVALID_INPUT when the value is uninitialized", value: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.ErrorIs(t, test.value.Validate(), test.wantErr)
		})
	}
}

func TestMoneyEqual(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	usd := money.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       money.Money
		wantResult bool
	}{
		{name: "should return true when amount and currency match", a: money.MustNew(1000, brl), b: money.MustNew(1000, brl), wantResult: true},
		{name: "should return false when amounts differ", a: money.MustNew(1000, brl), b: money.MustNew(2000, brl), wantResult: false},
		{name: "should return false when currencies differ", a: money.MustNew(1000, brl), b: money.MustNew(1000, usd), wantResult: false},
		{name: "should return false when zero values differ in currency", a: money.MustNew(0, brl), b: money.MustNew(0, usd), wantResult: false},
		{name: "should return true when both values are uninitialized", a: money.Money{}, b: money.Money{}, wantResult: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantResult, test.a.Equal(test.b))
		})
	}
}

func TestMoneyAmount(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      money.Money
		wantResult string
	}{
		{name: "should format with two decimals when the value has whole units", value: money.MustNew(2500, brl), wantResult: "25.00"},
		{name: "should format with two decimals when the value has cents", value: money.MustNew(2550, brl), wantResult: "25.50"},
		{name: "should format with a leading zero when the value is cents only", value: money.MustNew(7, brl), wantResult: "0.07"},
		{name: "should format as 0.00 when the value is zero", value: money.MustNew(0, brl), wantResult: "0.00"},
		{name: "should format with a minus sign when the value is negative", value: money.MustNew(-2550, brl), wantResult: "-25.50"},
		{name: "should format with a minus sign when the value is negative cents only", value: money.MustNew(-7, brl), wantResult: "-0.07"},
		{name: "should format the full number when the value is at the upper bound", value: money.MustNew(math.MaxInt64, brl), wantResult: "92233720368547758.07"},
		{name: "should format the full number when the value is at the lower bound", value: money.MustNew(math.MinInt64, brl), wantResult: "-92233720368547758.08"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantResult, test.value.Amount())
		})
	}
}

func TestMoneyPredicates(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name         string
		value        money.Money
		wantZero     bool
		wantPositive bool
		wantNegative bool
	}{
		{name: "should report zero when the value is zero", value: money.MustNew(0, brl), wantZero: true},
		{name: "should report positive when the value is above zero", value: money.MustNew(1, brl), wantPositive: true},
		{name: "should report negative when the value is below zero", value: money.MustNew(-1, brl), wantNegative: true},
		{name: "should report positive when the value is at the upper bound", value: money.MustNew(math.MaxInt64, brl), wantPositive: true},
		{name: "should report negative when the value is at the lower bound", value: money.MustNew(math.MinInt64, brl), wantNegative: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantZero, test.value.IsZero(), "IsZero")
			assert.Equal(t, test.wantPositive, test.value.IsPositive(), "IsPositive")
			assert.Equal(t, test.wantNegative, test.value.IsNegative(), "IsNegative")
		})
	}
}

func TestMoneyMarshalJSON(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      money.Money
		wantResult string
		wantErr    error
	}{
		{name: "should serialize the contract when the value is positive", value: money.MustNew(2500, brl), wantResult: `{"amount":"25.00","currency":"BRL"}`},
		{name: "should serialize with a minus sign when the value is negative", value: money.MustNew(-2500, brl), wantResult: `{"amount":"-25.00","currency":"BRL"}`},
		{name: "should serialize the contract when the value is zero", value: money.MustNew(0, money.MustCurrency("USD")), wantResult: `{"amount":"0.00","currency":"USD"}`},
		{name: "should return INVALID_INPUT when the value is uninitialized", value: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(test.value)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, string(got))
		})
	}
}

func TestMoneyJSONDecodingLeavesValueUninitialized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "should leave Money uninitialized when the payload holds a positive amount", data: `{"amount":"25.00","currency":"BRL"}`},
		{name: "should leave Money uninitialized when the payload holds a negative amount", data: `{"amount":"-25.00","currency":"BRL"}`},
		{name: "should leave Money uninitialized when the payload has a numeric amount", data: `{"amount":25.00}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got money.Money
			require.NoError(t, json.Unmarshal([]byte(test.data), &got))

			assert.Equal(t, money.Money{}, got)
			assert.ErrorIs(t, got.Validate(), domain.FailureCodeInvalidInput)
		})
	}
}
