package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
)

type PacketMetadata struct {
	Forward *ForwardMetadata `json:"forward"`
}

type ForwardMetadata struct {
	Receiver string   `json:"receiver,omitempty"`
	Port     string   `json:"port,omitempty"`
	Channel  string   `json:"channel,omitempty"`
	Timeout  Duration `json:"timeout,omitempty"`
	Retries  *uint8   `json:"retries,omitempty"`

	// Next is the memo of the forwarded packet, kept as bytes (see JSONObject).
	Next *JSONObject `json:"next,omitempty"`
}

type Duration time.Duration

// maxDurationFloat is 2^63. A float64 v converts to an int64 with a result
// that the Go specification defines only when -2^63 <= v < 2^63.
const maxDurationFloat = float64(1 << 63)

func (m *ForwardMetadata) Validate() error {
	if m.Receiver == "" {
		return fmt.Errorf("failed to validate metadata. receiver cannot be empty")
	}
	if err := host.PortIdentifierValidator(m.Port); err != nil {
		return fmt.Errorf("failed to validate metadata: %w", err)
	}
	if err := host.ChannelIdentifierValidator(m.Channel); err != nil {
		return fmt.Errorf("failed to validate metadata: %w", err)
	}
	if m.Next != nil && !json.Valid(m.Next.memo) {
		return fmt.Errorf("failed to validate metadata: next is not JSON")
	}

	return nil
}

// JSONObject holds the value of forward.next, which is the memo of the
// forwarded packet. A JSON string is kept as its unquoted content, so that an
// escaped JSON-marshaled string is also valid. Any other value is kept as the
// sender's bytes. The memo is never decoded and encoded again, so the work is
// linear in its length, and key order, whitespace and escapes stay as sent.
type JSONObject struct {
	memo []byte
}

// NewJSONObject returns a JSONObject that holds memo, the memo of the
// forwarded packet.
func NewJSONObject(memo []byte) *JSONObject {
	return &JSONObject{memo: memo}
}

// Memo returns the memo of the forwarded packet.
func (o *JSONObject) Memo() string {
	return string(o.memo)
}

// UnmarshalJSON overrides the default json.Unmarshal behavior
func (o *JSONObject) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		o.memo = []byte(s)
		return nil
	}
	// b is only valid during the call, so copy it.
	o.memo = append([]byte(nil), b...)
	return nil
}

// MarshalJSON overrides the default json.Marshal behavior. It returns the
// memo, which fails to encode if the memo is not JSON.
func (o JSONObject) MarshalJSON() ([]byte, error) {
	return o.memo, nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Nanoseconds())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		// Go leaves the conversion of a float64 outside the int64 range
		// implementation-defined, so it differs between CPU architectures.
		// encoding/json never decodes NaN or an infinity.
		if value < -maxDurationFloat || value >= maxDurationFloat {
			return errors.New("invalid duration: number out of range")
		}
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}
