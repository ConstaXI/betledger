package money

import "github.com/davibanfi/betledger/internal/domain"

const currencyCodeLen = 3

// Currency is a validated ISO 4217 code.
type Currency struct {
	// code is the three-letter uppercase ISO 4217 code, empty when uninitialized.
	code string
}

// NewCurrency validates an ISO 4217 code of three uppercase letters.
func NewCurrency(code string) (Currency, error) {
	if len(code) != currencyCodeLen {
		return Currency{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"currency %q is invalid: expected an ISO 4217 code with %d letters", code, currencyCodeLen)
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return Currency{}, domain.ValidationError(domain.FailureCodeInvalidInput,
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
