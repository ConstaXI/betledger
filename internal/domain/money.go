// Package domain holds the betledger business model, independent of Fx, HTTP,
// SQS and persistence libraries.
package domain

import (
	"encoding/json"
	"math"
	"strings"
)

const (
	// MoneyScale is the fixed number of decimal places of the external contract.
	MoneyScale       = 2
	moneyScaleFactor = 100
	currencyCodeLen  = 3
)

// Currency is a validated ISO 4217 code.
type Currency struct {
	// code is the three-letter uppercase ISO 4217 code, empty when uninitialized.
	code string
}

// NewCurrency validates an ISO 4217 code of three uppercase letters.
func NewCurrency(code string) (Currency, error) {
	if len(code) != currencyCodeLen {
		return Currency{}, ValidationError(FailureCodeInvalidInput,
			"currency %q is invalid: expected an ISO 4217 code with %d letters", code, currencyCodeLen)
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return Currency{}, ValidationError(FailureCodeInvalidInput,
				"currency %q is invalid: only uppercase letters are accepted", code)
		}
	}
	return Currency{code: code}, nil
}

// MustCurrency panics on an invalid code. Use NewCurrency for external input.
func MustCurrency(code string) Currency {
	currency, err := NewCurrency(code)
	if err != nil {
		panic(err)
	}
	return currency
}

func (c Currency) String() string { return c.code }

// IsZero reports whether the currency is uninitialized.
func (c Currency) IsZero() bool { return c.code == "" }

// Money is an immutable value object holding an amount and its currency, stored
// as int64 minor units with a fixed scale of two decimal places. The
// representable range is roughly ±92,233,720,368,547,758.07; operations that
// would exceed it return an error instead of truncating. No step uses floating
// point.
//
// Parsing spans ±MaxInt64 minor units. MinInt64 is reachable only through
// NewMoney, so its Amount is not parseable back; the domain invariants keep
// balances away from that bound.
type Money struct {
	// minorUnits is the amount in the smallest currency unit, at a fixed scale of
	// two decimal places.
	minorUnits int64
	// currency is empty only for an uninitialized Money, which every operation
	// rejects.
	currency Currency
}

// ParseMoney builds Money from the external contract, accepting only
// non-negative values. Equivalent forms ("25", "25.5", "25.50") are normalized
// to the fixed scale, and the normalized form is the one used for the
// idempotency hash.
func ParseMoney(amount, currencyCode string) (Money, error) {
	return parseMoney(amount, currencyCode, false)
}

// ParseSignedMoney also accepts negative values, for internal uses such as
// reconciliation differences rather than external financial input.
func ParseSignedMoney(amount, currencyCode string) (Money, error) {
	return parseMoney(amount, currencyCode, true)
}

func parseMoney(amount, currencyCode string, allowNegative bool) (Money, error) {
	currency, err := NewCurrency(currencyCode)
	if err != nil {
		return Money{}, err
	}
	minorUnits, err := parseMinorUnits(amount, allowNegative)
	if err != nil {
		return Money{}, err
	}
	return Money{minorUnits: minorUnits, currency: currency}, nil
}

// NewMoney builds Money from already validated minor units, as when rehydrating
// from the database.
func NewMoney(minorUnits int64, currency Currency) (Money, error) {
	if currency.IsZero() {
		return Money{}, ValidationError(FailureCodeInvalidInput, "currency is uninitialized")
	}
	return Money{minorUnits: minorUnits, currency: currency}, nil
}

// MustMoney panics when the currency is uninitialized. Use NewMoney outside of
// tests and static initialization.
func MustMoney(minorUnits int64, currency Currency) Money {
	money, err := NewMoney(minorUnits, currency)
	if err != nil {
		panic(err)
	}
	return money
}

// ZeroMoney returns the zero value of the given currency.
func ZeroMoney(currency Currency) (Money, error) {
	return NewMoney(0, currency)
}

func parseMinorUnits(amount string, allowNegative bool) (int64, error) {
	if amount == "" {
		return 0, ValidationError(FailureCodeInvalidAmount, "empty monetary amount")
	}

	digits := amount
	negative := false
	if strings.HasPrefix(digits, "-") {
		if !allowNegative {
			return 0, ValidationError(FailureCodeInvalidAmount,
				"monetary amount %q cannot be negative", amount)
		}
		negative = true
		digits = digits[1:]
	}

	intPart, fracPart := digits, ""
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		intPart, fracPart = digits[:i], digits[i+1:]
		if strings.IndexByte(fracPart, '.') >= 0 {
			return 0, ValidationError(FailureCodeInvalidAmount,
				"monetary amount %q has more than one decimal separator", amount)
		}
		if fracPart == "" {
			return 0, ValidationError(FailureCodeInvalidAmount,
				"monetary amount %q has a decimal separator without decimals", amount)
		}
		if len(fracPart) > MoneyScale {
			return 0, ValidationError(FailureCodeInvalidAmount,
				"monetary amount %q exceeds the scale of %d decimal places", amount, MoneyScale)
		}
	}
	if intPart == "" {
		return 0, ValidationError(FailureCodeInvalidAmount,
			"monetary amount %q has no integer part", amount)
	}

	combined := intPart + fracPart + strings.Repeat("0", MoneyScale-len(fracPart))

	var minorUnits int64
	for i := 0; i < len(combined); i++ {
		char := combined[i]
		if char < '0' || char > '9' {
			return 0, ValidationError(FailureCodeInvalidAmount,
				"monetary amount %q contains an invalid character", amount)
		}
		digit := int64(char - '0')
		if minorUnits > (math.MaxInt64-digit)/10 {
			return 0, ValidationError(FailureCodeAmountOverflow,
				"monetary amount %q exceeds the representable range", amount)
		}
		minorUnits = minorUnits*10 + digit
	}

	if negative {
		minorUnits = -minorUnits
	}
	return minorUnits, nil
}

// Validate rejects an uninitialized Money.
func (m Money) Validate() error {
	if m.currency.IsZero() {
		return ValidationError(FailureCodeInvalidInput, "monetary amount is uninitialized")
	}
	return nil
}

// Currency returns the currency of the value.
func (m Money) Currency() Currency { return m.currency }

// MinorUnits returns the value in minor units, as persisted.
func (m Money) MinorUnits() int64 { return m.minorUnits }

// Amount returns the decimal value at the fixed scale of two places.
func (m Money) Amount() string {
	units := m.minorUnits
	sign := ""
	if units < 0 {
		sign = "-"
	}
	var abs uint64
	if units < 0 {
		abs = uint64(-(units + 1)) + 1
	} else {
		abs = uint64(units)
	}

	whole := abs / moneyScaleFactor
	frac := abs % moneyScaleFactor
	var builder strings.Builder
	builder.WriteString(sign)
	builder.WriteString(formatUint(whole))
	builder.WriteByte('.')
	builder.WriteByte(byte('0' + frac/10))
	builder.WriteByte(byte('0' + frac%10))
	return builder.String()
}

func formatUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}

func (m Money) String() string {
	if m.currency.IsZero() {
		return m.Amount() + " <uninitialized currency>"
	}
	return m.Amount() + " " + m.currency.String()
}

// Add sums two values of the same currency.
func (m Money) Add(other Money) (Money, error) {
	if err := m.requireSameCurrency(other); err != nil {
		return Money{}, err
	}
	if (other.minorUnits > 0 && m.minorUnits > math.MaxInt64-other.minorUnits) ||
		(other.minorUnits < 0 && m.minorUnits < math.MinInt64-other.minorUnits) {
		return Money{}, ValidationError(FailureCodeAmountOverflow,
			"adding %s to %s exceeds the representable range", m, other)
	}
	return Money{minorUnits: m.minorUnits + other.minorUnits, currency: m.currency}, nil
}

// Sub subtracts two values of the same currency. The result may be negative.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.requireSameCurrency(other); err != nil {
		return Money{}, err
	}
	if (other.minorUnits < 0 && m.minorUnits > math.MaxInt64+other.minorUnits) ||
		(other.minorUnits > 0 && m.minorUnits < math.MinInt64+other.minorUnits) {
		return Money{}, ValidationError(FailureCodeAmountOverflow,
			"subtracting %s from %s exceeds the representable range", m, other)
	}
	return Money{minorUnits: m.minorUnits - other.minorUnits, currency: m.currency}, nil
}

// Neg returns the value with the opposite sign.
func (m Money) Neg() (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	if m.minorUnits == math.MinInt64 {
		return Money{}, ValidationError(FailureCodeAmountOverflow,
			"negating %s exceeds the representable range", m)
	}
	return Money{minorUnits: -m.minorUnits, currency: m.currency}, nil
}

// Cmp compares two values of the same currency, returning -1, 0 or 1.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.requireSameCurrency(other); err != nil {
		return 0, err
	}
	switch {
	case m.minorUnits < other.minorUnits:
		return -1, nil
	case m.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports whether amount and currency are identical. Values in different
// currencies are never equal.
func (m Money) Equal(other Money) bool { return m == other }

// IsZero reports whether the amount is zero. It does not distinguish an
// uninitialized Money; use Validate for that.
func (m Money) IsZero() bool { return m.minorUnits == 0 }

// IsPositive reports whether the amount is greater than zero.
func (m Money) IsPositive() bool { return m.minorUnits > 0 }

// IsNegative reports whether the amount is less than zero.
func (m Money) IsNegative() bool { return m.minorUnits < 0 }

func (m Money) requireSameCurrency(other Money) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := other.Validate(); err != nil {
		return err
	}
	if m.currency != other.currency {
		return ValidationError(FailureCodeCurrencyMismatch,
			"incompatible currencies: %s and %s", m.currency, other.currency)
	}
	return nil
}

type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON serializes to the external contract
// {"amount":"25.00","currency":"BRL"}. It exists because the fields are
// unexported; decoding is deliberately absent, so external input always goes
// through ParseMoney.
func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(moneyJSON{Amount: m.Amount(), Currency: m.currency.String()})
}
