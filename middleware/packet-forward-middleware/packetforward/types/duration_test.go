package types_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/stretchr/testify/require"
)

// TestDurationUnmarshalNumberRange checks that a numeric timeout must fit in
// an int64 number of nanoseconds. Go leaves the conversion of an
// out-of-range float64 to int64 implementation-defined, so the result would
// differ between CPU architectures.
func TestDurationUnmarshalNumberRange(t *testing.T) {
	testCases := []struct {
		json    string
		want    time.Duration
		wantErr bool
	}{
		{json: `0`, want: 0},
		{json: `-0`, want: 0},
		{json: `1.5`, want: 1},
		{json: `-5`, want: -5},
		{json: `600000000000`, want: 10 * time.Minute},
		{json: `-9223372036854775808`, want: time.Duration(-1 << 63)},
		{json: `9223372036854774784`, want: 9223372036854774784}, // largest float64 below 2^63
		{json: `"10m"`, want: 10 * time.Minute},
		{json: `9223372036854775807`, wantErr: true}, // parses to the float64 2^63
		{json: `9223372036854775808`, wantErr: true},
		{json: `10000000000000000000`, wantErr: true},
		{json: `9.3e18`, wantErr: true},
		{json: `1e19`, wantErr: true},
		{json: `1E+19`, wantErr: true},
		{json: `-1e19`, wantErr: true},
		{json: `-9223372036854777856`, wantErr: true}, // next float64 below -2^63
		{json: `1e308`, wantErr: true},
		{json: `1e400`, wantErr: true},
		{json: `"1e19"`, wantErr: true},
		{json: `true`, wantErr: true},
		{json: `[1]`, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.json, func(t *testing.T) {
			var d types.Duration
			err := json.Unmarshal([]byte(tc.json), &d)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, time.Duration(d))
		})
	}
}

// TestPacketMetadataOutOfRangeTimeout checks that a forward memo with an
// out-of-range timeout fails to decode, also in the field name case and
// repeated-key forms that encoding/json accepts.
func TestPacketMetadataOutOfRangeTimeout(t *testing.T) {
	for _, memo := range []string{
		`{"forward":{"receiver":"r","timeout":1e19}}`,
		`{"forward":{"receiver":"r","TimeOut":1e19}}`,
		`{"forward":{"receiver":"r","timeout":5,"timeout":-1e19}}`,
	} {
		var m types.PacketMetadata
		require.Error(t, json.Unmarshal([]byte(memo), &m), memo)
	}
}
