package automation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/OboardProject/oboard/internal/security"
)

// Sealed input keeps a write-only operation value, such as a plugin instance
// secret, out of every persisted changeset. The value is encrypted with the
// Controller session secret before the operation is stored and decrypted only
// when it is handed to the capability's validator or handler. The stored form
// is "sealed:v1:<fingerprint>:<ciphertext>"; the fingerprint is a keyed HMAC so
// idempotent replays compare equal without decrypting, and it never reveals
// the value.
const (
	sealedInputPrefix  = "sealed:v1:"
	sealedInputPurpose = "automation-sealed-input"
)

var errSealedInputUnavailable = errors.New("sealed input is unavailable in this Controller build")

// SetInputSealKey enables sealed input. Without a key, capabilities that
// declare sealed fields are refused instead of storing the value in clear.
func (s *Service) SetInputSealKey(key string) {
	s.mu.Lock()
	s.sealKey = key
	s.mu.Unlock()
}

func (s *Service) inputSealKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sealKey
}

func (s *Service) sealInput(input json.RawMessage, fields []string) (json.RawMessage, error) {
	if len(fields) == 0 {
		return input, nil
	}
	key := s.inputSealKey()
	if key == "" {
		return nil, errSealedInputUnavailable
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return nil, err
	}
	for _, field := range fields {
		raw, ok := object[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, errors.New(field + " must be a string")
		}
		if strings.HasPrefix(value, sealedInputPrefix) {
			return nil, errors.New(field + " must not be a sealed value")
		}
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(sealedInputPurpose + "\x00" + field + "\x00" + value))
		ciphertext, err := security.EncryptSecret(key, sealedInputPurpose, value)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(sealedInputPrefix + hex.EncodeToString(mac.Sum(nil)) + ":" + ciphertext)
		if err != nil {
			return nil, err
		}
		object[field] = encoded
	}
	sealed, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return canonicalObject(sealed)
}

// openInput returns the operation input with sealed fields decrypted. It is
// used only on the way into a validator or handler; the result is never
// stored.
func (s *Service) openInput(capabilityName string, input json.RawMessage) (json.RawMessage, error) {
	descriptor, ok := s.catalog.Get(capabilityName)
	if !ok || len(descriptor.SealedInput) == 0 {
		return input, nil
	}
	key := s.inputSealKey()
	if key == "" {
		return nil, errSealedInputUnavailable
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return nil, err
	}
	for _, field := range descriptor.SealedInput {
		raw, ok := object[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, errors.New(field + " must be a string")
		}
		if !strings.HasPrefix(value, sealedInputPrefix) {
			// A draft that is validated without being stored still carries
			// the caller's value; only stored operations are sealed.
			continue
		}
		rest := strings.TrimPrefix(value, sealedInputPrefix)
		separator := strings.IndexByte(rest, ':')
		if separator < 0 {
			return nil, errors.New(field + " is not sealed")
		}
		plain, err := security.DecryptSecret(key, sealedInputPurpose, rest[separator+1:])
		if err != nil {
			return nil, errors.New(field + " cannot be unsealed")
		}
		encoded, err := json.Marshal(plain)
		if err != nil {
			return nil, err
		}
		object[field] = encoded
	}
	return json.Marshal(object)
}

// sealedDigestForm drops the ciphertext from sealed values so two seals of
// the same value compare equal in the request digest.
func sealedDigestForm(input json.RawMessage) json.RawMessage {
	if !strings.Contains(string(input), sealedInputPrefix) {
		return input
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return input
	}
	for field, raw := range object {
		var value string
		if json.Unmarshal(raw, &value) != nil || !strings.HasPrefix(value, sealedInputPrefix) {
			continue
		}
		rest := strings.TrimPrefix(value, sealedInputPrefix)
		if separator := strings.IndexByte(rest, ':'); separator >= 0 {
			rest = rest[:separator]
		}
		encoded, _ := json.Marshal(sealedInputPrefix + rest)
		object[field] = encoded
	}
	out, err := json.Marshal(object)
	if err != nil {
		return input
	}
	return out
}
