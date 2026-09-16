package domain_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
)

func TestParseMoney(t *testing.T) {
	t.Parallel()

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		amount     string
		currency   string
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should accept when amount has full scale", amount: "25.00", currency: "BRL", wantResult: domain.MustMoney(2500, brl)},
		{name: "should accept when amount has no decimals", amount: "25", currency: "BRL", wantResult: domain.MustMoney(2500, brl)},
		{name: "should normalize scale when amount has one decimal place", amount: "25.5", currency: "BRL", wantResult: domain.MustMoney(2550, brl)},
		{name: "should accept when amount is zero", amount: "0.00", currency: "BRL", wantResult: domain.MustMoney(0, brl)},
		{name: "should accept when zero has no decimals", amount: "0", currency: "BRL", wantResult: domain.MustMoney(0, brl)},
		{name: "should accept when amount has only cents", amount: "0.07", currency: "BRL", wantResult: domain.MustMoney(7, brl)},
		{name: "should accept when amount has leading zeros", amount: "0025.00", currency: "BRL", wantResult: domain.MustMoney(2500, brl)},
		{name: "should accept when amount is at the upper bound", amount: "92233720368547758.07", currency: "BRL", wantResult: domain.MustMoney(math.MaxInt64, brl)},
		{name: "should accept when currency is not BRL", amount: "25.00", currency: "USD", wantResult: domain.MustMoney(2500, domain.MustCurrency("USD"))},

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

			got, err := domain.ParseMoney(test.amount, test.currency)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestParseSignedMoney(t *testing.T) {
	t.Parallel()

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		amount     string
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should accept when amount is negative", amount: "-25.50", wantResult: domain.MustMoney(-2550, brl)},
		{name: "should accept when amount is negative cents", amount: "-0.01", wantResult: domain.MustMoney(-1, brl)},
		{name: "should accept when amount is positive", amount: "25.50", wantResult: domain.MustMoney(2550, brl)},
		{name: "should normalize to zero when amount is negative zero", amount: "-0.00", wantResult: domain.MustMoney(0, brl)},
		{name: "should accept when amount is the negated upper bound", amount: "-92233720368547758.07", wantResult: domain.MustMoney(-math.MaxInt64, brl)},
		{name: "should return AMOUNT_OVERFLOW when amount is at the lower bound", amount: "-92233720368547758.08", wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_AMOUNT when amount is a lone minus sign", amount: "-", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when a negative amount exceeds the scale", amount: "-25.123", wantErr: domain.FailureCodeInvalidAmount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseSignedMoney(test.amount, "BRL")

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestMoneyAdd(t *testing.T) {
	t.Parallel()

	brl := domain.MustCurrency("BRL")
	usd := domain.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       domain.Money
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should return the sum when both values are positive", a: domain.MustMoney(10000, brl), b: domain.MustMoney(8000, brl), wantResult: domain.MustMoney(18000, brl)},
		{name: "should return the sum when the operand is negative", a: domain.MustMoney(10000, brl), b: domain.MustMoney(-8000, brl), wantResult: domain.MustMoney(2000, brl)},
		{name: "should return the receiver when the operand is zero", a: domain.MustMoney(10000, brl), b: domain.MustMoney(0, brl), wantResult: domain.MustMoney(10000, brl)},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return AMOUNT_OVERFLOW when the sum is above the upper bound", a: domain.MustMoney(math.MaxInt64, brl), b: domain.MustMoney(1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return AMOUNT_OVERFLOW when the sum is below the lower bound", a: domain.MustMoney(math.MinInt64, brl), b: domain.MustMoney(-1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: domain.Money{}, b: domain.MustMoney(1000, brl), wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the operand is uninitialized", a: domain.MustMoney(1000, brl), b: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
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

	brl := domain.MustCurrency("BRL")
	usd := domain.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       domain.Money
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should return the difference when the result is positive", a: domain.MustMoney(10000, brl), b: domain.MustMoney(8000, brl), wantResult: domain.MustMoney(2000, brl)},
		{name: "should return the difference when the result is negative", a: domain.MustMoney(8000, brl), b: domain.MustMoney(10000, brl), wantResult: domain.MustMoney(-2000, brl)},
		{name: "should return zero when both values are equal", a: domain.MustMoney(10000, brl), b: domain.MustMoney(10000, brl), wantResult: domain.MustMoney(0, brl)},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return AMOUNT_OVERFLOW when the difference is below the lower bound", a: domain.MustMoney(math.MinInt64, brl), b: domain.MustMoney(1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return AMOUNT_OVERFLOW when the difference is above the upper bound", a: domain.MustMoney(math.MaxInt64, brl), b: domain.MustMoney(-1, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: domain.Money{}, b: domain.MustMoney(1000, brl), wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the operand is uninitialized", a: domain.MustMoney(1000, brl), b: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
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

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      domain.Money
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should return a negative value when the receiver is positive", value: domain.MustMoney(8000, brl), wantResult: domain.MustMoney(-8000, brl)},
		{name: "should return a positive value when the receiver is negative", value: domain.MustMoney(-8000, brl), wantResult: domain.MustMoney(8000, brl)},
		{name: "should return zero when the receiver is zero", value: domain.MustMoney(0, brl), wantResult: domain.MustMoney(0, brl)},
		{name: "should return the negated value when the receiver is at the upper bound", value: domain.MustMoney(math.MaxInt64, brl), wantResult: domain.MustMoney(-math.MaxInt64, brl)},
		{name: "should return AMOUNT_OVERFLOW when the receiver is at the lower bound", value: domain.MustMoney(math.MinInt64, brl), wantErr: domain.FailureCodeAmountOverflow},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", value: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
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

	brl := domain.MustCurrency("BRL")
	usd := domain.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       domain.Money
		wantResult int
		wantErr    error
	}{
		{name: "should return -1 when the receiver is smaller", a: domain.MustMoney(1000, brl), b: domain.MustMoney(2000, brl), wantResult: -1},
		{name: "should return 1 when the receiver is greater", a: domain.MustMoney(2000, brl), b: domain.MustMoney(1000, brl), wantResult: 1},
		{name: "should return 0 when both values are equal", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, brl), wantResult: 0},
		{name: "should return -1 when the receiver is negative and the operand is positive", a: domain.MustMoney(-1000, brl), b: domain.MustMoney(1000, brl), wantResult: -1},
		{name: "should return CURRENCY_MISMATCH when operands differ in currency", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, usd), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return INVALID_INPUT when the receiver is uninitialized", a: domain.Money{}, b: domain.MustMoney(1000, brl), wantErr: domain.FailureCodeInvalidInput},
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

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name    string
		value   domain.Money
		wantErr error
	}{
		{name: "should accept when the value is initialized", value: domain.MustMoney(1000, brl)},
		{name: "should accept when the value is initialized at zero", value: domain.MustMoney(0, brl)},
		{name: "should return INVALID_INPUT when the value is uninitialized", value: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
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

	brl := domain.MustCurrency("BRL")
	usd := domain.MustCurrency("USD")

	tests := []struct {
		name       string
		a, b       domain.Money
		wantResult bool
	}{
		{name: "should return true when amount and currency match", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, brl), wantResult: true},
		{name: "should return false when amounts differ", a: domain.MustMoney(1000, brl), b: domain.MustMoney(2000, brl), wantResult: false},
		{name: "should return false when currencies differ", a: domain.MustMoney(1000, brl), b: domain.MustMoney(1000, usd), wantResult: false},
		{name: "should return false when zero values differ in currency", a: domain.MustMoney(0, brl), b: domain.MustMoney(0, usd), wantResult: false},
		{name: "should return true when both values are uninitialized", a: domain.Money{}, b: domain.Money{}, wantResult: true},
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

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      domain.Money
		wantResult string
	}{
		{name: "should format with two decimals when the value has whole units", value: domain.MustMoney(2500, brl), wantResult: "25.00"},
		{name: "should format with two decimals when the value has cents", value: domain.MustMoney(2550, brl), wantResult: "25.50"},
		{name: "should format with a leading zero when the value is cents only", value: domain.MustMoney(7, brl), wantResult: "0.07"},
		{name: "should format as 0.00 when the value is zero", value: domain.MustMoney(0, brl), wantResult: "0.00"},
		{name: "should format with a minus sign when the value is negative", value: domain.MustMoney(-2550, brl), wantResult: "-25.50"},
		{name: "should format with a minus sign when the value is negative cents only", value: domain.MustMoney(-7, brl), wantResult: "-0.07"},
		{name: "should format the full number when the value is at the upper bound", value: domain.MustMoney(math.MaxInt64, brl), wantResult: "92233720368547758.07"},
		{name: "should format the full number when the value is at the lower bound", value: domain.MustMoney(math.MinInt64, brl), wantResult: "-92233720368547758.08"},
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

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name         string
		value        domain.Money
		wantZero     bool
		wantPositive bool
		wantNegative bool
	}{
		{name: "should report zero when the value is zero", value: domain.MustMoney(0, brl), wantZero: true},
		{name: "should report positive when the value is above zero", value: domain.MustMoney(1, brl), wantPositive: true},
		{name: "should report negative when the value is below zero", value: domain.MustMoney(-1, brl), wantNegative: true},
		{name: "should report positive when the value is at the upper bound", value: domain.MustMoney(math.MaxInt64, brl), wantPositive: true},
		{name: "should report negative when the value is at the lower bound", value: domain.MustMoney(math.MinInt64, brl), wantNegative: true},
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

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		value      domain.Money
		wantResult string
		wantErr    error
	}{
		{name: "should serialize the contract when the value is positive", value: domain.MustMoney(2500, brl), wantResult: `{"amount":"25.00","currency":"BRL"}`},
		{name: "should serialize with a minus sign when the value is negative", value: domain.MustMoney(-2500, brl), wantResult: `{"amount":"-25.00","currency":"BRL"}`},
		{name: "should serialize the contract when the value is zero", value: domain.MustMoney(0, domain.MustCurrency("USD")), wantResult: `{"amount":"0.00","currency":"USD"}`},
		{name: "should return INVALID_INPUT when the value is uninitialized", value: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
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

func TestMoneyUnmarshalJSON(t *testing.T) {
	t.Parallel()

	brl := domain.MustCurrency("BRL")

	tests := []struct {
		name       string
		data       string
		wantResult domain.Money
		wantErr    error
	}{
		{name: "should parse when the payload holds a positive amount", data: `{"amount":"25.00","currency":"BRL"}`, wantResult: domain.MustMoney(2500, brl)},
		{name: "should parse when the payload holds a negative amount", data: `{"amount":"-25.00","currency":"BRL"}`, wantResult: domain.MustMoney(-2500, brl)},
		{name: "should normalize scale when the payload has one decimal place", data: `{"amount":"25.5","currency":"BRL"}`, wantResult: domain.MustMoney(2550, brl)},
		{name: "should return INVALID_AMOUNT when the payload exceeds the scale", data: `{"amount":"25.123","currency":"BRL"}`, wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the payload currency is invalid", data: `{"amount":"25.00","currency":"brl"}`, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_AMOUNT when the payload has no amount", data: `{"currency":"BRL"}`, wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the payload is malformed", data: `{"amount":25.00}`, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got domain.Money
			err := json.Unmarshal([]byte(test.data), &got)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}
