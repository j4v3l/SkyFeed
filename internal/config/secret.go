package config

import "encoding/json"

const redacted = "[REDACTED]"

// Secret keeps sensitive configuration from being exposed through ordinary
// formatting or JSON marshaling. Reveal should be called only at the adapter
// boundary that consumes the secret.
type Secret struct {
	value string
}

func newSecret(value string) Secret {
	return Secret{value: value}
}

func (s Secret) Reveal() string {
	return s.value
}

func (Secret) String() string {
	return redacted
}

func (Secret) GoString() string {
	return redacted
}

func (Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(redacted)
}
