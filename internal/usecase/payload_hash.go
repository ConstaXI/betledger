package usecase

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// PayloadHash returns the hex SHA-256 of the canonical JSON of the business
// fields of an operation. Keys are sorted, the idempotency key and the
// correlation identifier are excluded, identifiers use their canonical form,
// money uses its normalized two-decimal amount, and an absent reference is
// omitted. The same operation therefore hashes identically over HTTP and SQS.
func PayloadHash(input ProcessWagerInput) (string, error) {
	if err := input.Money.Validate(); err != nil {
		return "", err
	}

	fields := map[string]any{
		"providerId":            input.ProviderID,
		"externalTransactionId": input.ExternalTransactionID,
		"playerId":              input.PlayerID.String(),
		"walletId":              input.WalletID.String(),
		"roundId":               input.RoundID,
		"gameId":                input.GameID,
		"kind":                  input.Kind.String(),
		"money": map[string]any{
			"amount":   input.Money.Amount(),
			"currency": input.Money.Currency().String(),
		},
	}
	if input.ReferenceExternalTransactionID != "" {
		fields["referenceExternalTransactionId"] = input.ReferenceExternalTransactionID
	}

	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(fields); err != nil {
		return "", err
	}

	sum := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:]), nil
}
