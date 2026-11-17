package packetforward_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

// forwardErrorEvents returns the attributes of each packet_forward_error
// event in events.
func forwardErrorEvents(events sdk.Events) []map[string]string {
	var out []map[string]string
	for _, e := range events {
		if e.Type != types.EventTypeForwardError {
			continue
		}
		attrs := make(map[string]string, len(e.Attributes))
		for _, a := range e.Attributes {
			attrs[a.Key] = a.Value
		}
		out = append(out, attrs)
	}
	return out
}

// TestOnRecvPacket_ErrorEvent checks that a rejected forward memo emits one
// packet_forward_error event with the cause and the packet identifiers,
// while the acknowledgement stays the fixed text. Events are not part of
// any consensus hash, so the cause can be Go error text.
func TestOnRecvPacket_ErrorEvent(t *testing.T) {
	for name, tc := range map[string]struct {
		memo  string
		cause string
	}{
		"invalid port":            {`{"forward":{"port":"x","channel":"channel-0","receiver":"r"}}`, "failed to validate metadata"},
		"timeout out of range":    {`{"forward":{"port":"transfer","channel":"channel-0","receiver":"r","timeout":1e19}}`, "error parsing forward metadata"},
		"forward decodes to null": {`{"forward":{"port":"transfer","channel":"channel-0","receiver":"r"},"Forward":null}`, "forward metadata is null"},
		"memo too long":           {forwardMemo(`,"x":"` + strings.Repeat("a", 40000) + `"`), "forward memo length"},
	} {
		t.Run(name, func(t *testing.T) {
			ctl := gomock.NewController(t)
			defer ctl.Finish()
			setup := test.NewTestSetup(t, ctl)
			ctx := setup.Initializer.Ctx.WithEventManager(sdk.NewEventManager())
			packet := transferPacket(t, senderAddr, hostAddr, tc.memo)
			packet.Sequence = 7

			ack := setup.ForwardMiddleware.OnRecvPacket(ctx, packet, test.AccAddress())
			require.Equal(t, fixedErrorAck, string(ack.Acknowledgement()))

			events := forwardErrorEvents(ctx.EventManager().Events())
			require.Len(t, events, 1)
			require.Contains(t, events[0][types.AttributeKeyError], tc.cause)
			require.Equal(t, "7", events[0][channeltypes.AttributeKeySequence])
			require.Equal(t, packet.DestinationPort, events[0][channeltypes.AttributeKeyDstPort])
			require.Equal(t, packet.DestinationChannel, events[0][channeltypes.AttributeKeyDstChannel])
		})
	}
}

// TestOnRecvPacket_NoErrorEventOnForward checks that a forwarded packet
// emits no packet_forward_error event.
func TestOnRecvPacket_NoErrorEventOnForward(t *testing.T) {
	f := newForwarder(t)
	ctx := f.setup.Initializer.Ctx.WithEventManager(sdk.NewEventManager())
	f.setup.Initializer.Ctx = ctx
	ack, _, forwarded := f.recvPacket(transferPacket(t, senderAddr, hostAddr, forwardMemo("")))
	require.Nil(t, ack)
	require.True(t, forwarded)
	require.Empty(t, forwardErrorEvents(ctx.EventManager().Events()))
}

// TestOnTimeoutPacket_NoRetriesLeftErrorEvent checks that the error
// acknowledgement written when no retries are left emits a
// packet_forward_error event that names the source packet, the packet the
// acknowledgement is for.
func TestOnTimeoutPacket_NoRetriesLeftErrorEvent(t *testing.T) {
	f := newForwarder(t)
	srcPacket := transferPacket(t, senderAddr, hostAddr, forwardMemo(`,"retries":0`))
	srcPacket.Sequence = 9
	ack, fwdMemo, forwarded := f.recvPacket(srcPacket)
	require.Nil(t, ack)
	require.True(t, forwarded)

	ctx := f.setup.Initializer.Ctx.WithEventManager(sdk.NewEventManager())
	f.setup.Initializer.Ctx = ctx
	f.expectRefund(srcPacket, sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	_, retried, err := f.timeoutPacket(f.forwardedPacket(fwdMemo))
	require.NoError(t, err)
	require.False(t, retried)

	events := forwardErrorEvents(ctx.EventManager().Events())
	require.Len(t, events, 1)
	require.NotEmpty(t, events[0][types.AttributeKeyError])
	require.Equal(t, strconv.FormatUint(srcPacket.Sequence, 10), events[0][channeltypes.AttributeKeySequence])
	require.Equal(t, srcPacket.DestinationPort, events[0][channeltypes.AttributeKeyDstPort])
	require.Equal(t, srcPacket.DestinationChannel, events[0][channeltypes.AttributeKeyDstChannel])
}
