package types_test

import (
	"encoding/json"
	"testing"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/stretchr/testify/require"
)

func TestForwardMetadataUnmarshalStringNext(t *testing.T) {
	const memo = "{\"forward\":{\"receiver\":\"noble1f4cur2krsua2th9kkp7n0zje4stea4p9tu70u8\",\"port\":\"transfer\",\"channel\":\"channel-0\",\"timeout\":0,\"next\":\"{\\\"forward\\\":{\\\"receiver\\\":\\\"noble1l505zhahp24v5jsmps9vs5asah759fdce06sfp\\\",\\\"port\\\":\\\"transfer\\\",\\\"channel\\\":\\\"channel-0\\\",\\\"timeout\\\":0}}\"}}"
	var packetMetadata types.PacketMetadata

	err := json.Unmarshal([]byte(memo), &packetMetadata)
	require.NoError(t, err)

	nextBz, err := json.Marshal(packetMetadata.Forward.Next)
	require.NoError(t, err)
	require.Equal(t, `{"forward":{"receiver":"noble1l505zhahp24v5jsmps9vs5asah759fdce06sfp","port":"transfer","channel":"channel-0","timeout":0}}`, string(nextBz))
}

func TestForwardMetadataUnmarshalJSONNext(t *testing.T) {
	const memo = "{\"forward\":{\"receiver\":\"noble1f4cur2krsua2th9kkp7n0zje4stea4p9tu70u8\",\"port\":\"transfer\",\"channel\":\"channel-0\",\"timeout\":0,\"next\":{\"forward\":{\"receiver\":\"noble1l505zhahp24v5jsmps9vs5asah759fdce06sfp\",\"port\":\"transfer\",\"channel\":\"channel-0\",\"timeout\":0}}}}"
	var packetMetadata types.PacketMetadata

	err := json.Unmarshal([]byte(memo), &packetMetadata)
	require.NoError(t, err)

	nextBz, err := json.Marshal(packetMetadata.Forward.Next)
	require.NoError(t, err)
	require.Equal(t, `{"forward":{"receiver":"noble1l505zhahp24v5jsmps9vs5asah759fdce06sfp","port":"transfer","channel":"channel-0","timeout":0}}`, string(nextBz))
}

func TestTimeoutUnmarshalString(t *testing.T) {
	const memo = "{\"forward\":{\"receiver\":\"noble1f4cur2krsua2th9kkp7n0zje4stea4p9tu70u8\",\"port\":\"transfer\",\"channel\":\"channel-0\",\"timeout\":\"60s\"}}"
	var packetMetadata types.PacketMetadata

	err := json.Unmarshal([]byte(memo), &packetMetadata)
	require.NoError(t, err)

	timeoutBz, err := json.Marshal(packetMetadata.Forward.Timeout)
	require.NoError(t, err)

	require.Equal(t, "60000000000", string(timeoutBz))
}

func TestTimeoutUnmarshalJSON(t *testing.T) {
	const memo = "{\"forward\":{\"receiver\":\"noble1f4cur2krsua2th9kkp7n0zje4stea4p9tu70u8\",\"port\":\"transfer\",\"channel\":\"channel-0\",\"timeout\": 60000000000}}"
	var packetMetadata types.PacketMetadata

	err := json.Unmarshal([]byte(memo), &packetMetadata)
	require.NoError(t, err)

	timeoutBz, err := json.Marshal(packetMetadata.Forward.Timeout)
	require.NoError(t, err)

	require.Equal(t, "60000000000", string(timeoutBz))
}

func TestForwardMetadataNextKeepsBytes(t *testing.T) {
	memo := []byte(`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next": { "b" : "<&>",  "a" : 1e20 } }}`)
	var packetMetadata types.PacketMetadata
	require.NoError(t, json.Unmarshal(memo, &packetMetadata))
	// The bytes are copied, not kept as a slice of the input.
	for i := range memo {
		memo[i] = ' '
	}
	require.Equal(t, `{ "b" : "<&>",  "a" : 1e20 }`, packetMetadata.Forward.Next.Memo())
	require.NoError(t, packetMetadata.Forward.Validate())

	require.NoError(t, json.Unmarshal([]byte(`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","next":"hello"}}`), &packetMetadata))
	require.Equal(t, "hello", packetMetadata.Forward.Next.Memo())
	require.Error(t, packetMetadata.Forward.Validate())
}
