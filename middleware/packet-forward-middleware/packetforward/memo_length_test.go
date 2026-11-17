package packetforward_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

// padForwardMemo returns a forward memo to channel-0 of n bytes, padded in
// forward.next.
func padForwardMemo(n int) string {
	return padTo(n, func(p string) string { return forwardMemo(nextKey + aObject(p)) })
}

// paddedForwardMemo returns a forward memo to channel-0 of exactly n bytes.
func paddedForwardMemo(t *testing.T, n int) string {
	t.Helper()
	memo := padForwardMemo(n)
	require.Len(t, memo, n)
	return memo
}

// TestOnRecvPacket_MemoLengthCap checks that a forward memo of at most
// ibc-go's MsgTransfer memo limit is forwarded, and that a longer one gets
// the fixed error acknowledgement before any funds are received or sent.
func TestOnRecvPacket_MemoLengthCap(t *testing.T) {
	t.Run("forward memo at the limit is forwarded", func(t *testing.T) {
		memo := paddedForwardMemo(t, maxMemoLength)
		f := newForwarder(t)
		ack, fwdMemo, forwarded := f.recv(memo)
		require.Nil(t, ack)
		require.True(t, forwarded)
		require.Less(t, len(fwdMemo), len(memo))
	})
	for name, memo := range map[string]string{
		"forward memo one byte over the limit": paddedForwardMemo(t, maxMemoLength+1),
		"forward memo of 1 MiB":                paddedForwardMemo(t, 1<<20),
		// Valid forward metadata with the padding in a key next to
		// "forward": the forwarded memo would be short, so only the cap on
		// the whole memo rejects it.
		"forward memo over the limit, padded outside forward": padTo(maxMemoLength+1, func(p string) string {
			return forwardReceiverPrefix + destAddr + `","port":"transfer","channel":"channel-0"},"a":"` + p + `"}`
		}),
	} {
		t.Run(name, func(t *testing.T) {
			// No mock is expected: a call into the transfer app (receiving
			// funds) or the transfer keeper (sending them) fails the test.
			ack := recvWithoutForwarding(t, memo)
			require.NotNil(t, ack)
			require.Equal(t, fixedErrorAck, string(ack.Acknowledgement()))
		})
	}
}

// TestOnRecvPacket_LongMemoWithoutForwardIsUnchanged checks that a memo over
// the limit that is not a forward memo goes to the transfer app unchanged.
func TestOnRecvPacket_LongMemoWithoutForwardIsUnchanged(t *testing.T) {
	const n = 40 << 10
	for name, memo := range map[string]string{
		"plain text":         strings.Repeat("r", n),
		"JSON, no forward":   padTo(n, func(p string) string { return `{"wasm":{"msg":"` + p + `"}}` }),
		"JSON, forward null": padTo(n, func(p string) string { return `{"forward":null,"a":"` + p + `"}` }),
		"JSON, Forward":      padTo(n, func(p string) string { return `{"Forward":{"receiver":"r"},"a":"` + p + `"}` }),
		"invalid JSON":       forwardKey + strings.Repeat("r", n),
	} {
		t.Run(name, func(t *testing.T) {
			require.Greater(t, len(memo), maxMemoLength)
			ctl := gomock.NewController(t)
			defer ctl.Finish()
			setup := test.NewTestSetup(t, ctl)
			packet := transferPacket(t, senderAddr, hostAddr, memo)
			want := channeltypes.NewResultAcknowledgement([]byte{1})
			setup.Mocks.IBCModuleMock.EXPECT().OnRecvPacket(setup.Initializer.Ctx, packet, relayerAddr).Return(want)
			ack := setup.ForwardMiddleware.OnRecvPacket(setup.Initializer.Ctx, packet, relayerAddr)
			require.Equal(t, want, ack)
		})
	}
}

// TestRetryOverLimitInFlightMemo checks that a packet forwarded before the
// memo limit existed, with a memo over the limit, is still sent again as is
// on each timeout, and is refunded when no retries are left.
func TestRetryOverLimitInFlightMemo(t *testing.T) {
	f := newForwarder(t)
	ctx := f.setup.Initializer.Ctx
	next := aObject(strings.Repeat("r", 40<<10))
	srcPacket := transferPacket(t, senderAddr, hostAddr, forwardMemo(nextKey+next))
	require.Greater(t, len(next), maxMemoLength)

	// Forward the packet as a version without the limit did.
	metadata := &types.ForwardMetadata{
		Receiver: destAddr, Port: port, Channel: channel,
		Next: types.NewJSONObject([]byte(next)),
	}
	token := sdk.NewCoin(testDenom, sdkmath.NewInt(100))
	require.NoError(t, f.setup.Keepers.PacketForwardKeeper.ForwardTransferPacket(
		ctx, nil, srcPacket, senderAddr, intermediateAddr, metadata, token, 2, time.Nanosecond, nil, false,
	))
	require.Equal(t, []string{next}, f.memos)

	for i := 0; i < 2; i++ {
		retried, ok, err := f.timeout(next)
		require.NoError(t, err)
		require.True(t, ok, "the packet must be sent again")
		require.Equal(t, next, retried)
	}

	// No retries are left: the funds go back to the escrow of the source
	// channel and the source packet gets the fixed error acknowledgement.
	f.expectRefund(srcPacket, token)
	_, ok, err := f.timeout(next)
	require.NoError(t, err)
	require.False(t, ok, "no retries are left")
}

// expectRefund expects, once, the refund of token for srcPacket after a
// forwarded packet on channel-0 fails: the funds go back to the escrow of
// the source channel and srcPacket gets exactly the fixed error
// acknowledgement.
func (f *forwarder) expectRefund(srcPacket channeltypes.Packet, token sdk.Coin) {
	t := f.t
	t.Helper()
	m := f.setup.Mocks
	ctx := f.setup.Initializer.Ctx
	chanCap := capabilitytypes.NewCapability(1)
	m.ChannelKeeperMock.EXPECT().LookupModuleByChannel(ctx, srcPacket.DestinationPort, srcPacket.DestinationChannel).
		Return(transfertypes.ModuleName, chanCap, nil)
	m.BankKeeperMock.EXPECT().SendCoins(gomock.Any(),
		transfertypes.GetEscrowAddress(port, channel),
		transfertypes.GetEscrowAddress(srcPacket.DestinationPort, srcPacket.DestinationChannel),
		sdk.NewCoins(token)).Return(nil)
	m.ICS4WrapperMock.EXPECT().WriteAcknowledgement(ctx, chanCap, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ sdk.Context, _ *capabilitytypes.Capability, p ibcexported.PacketI, ack ibcexported.Acknowledgement) error {
			require.Equal(t, srcPacket.Data, p.GetData())
			require.Equal(t, srcPacket.DestinationChannel, p.GetDestChannel())
			require.Equal(t, fixedErrorAck, string(ack.Acknowledgement()))
			return nil
		})
}

// TestOnTimeoutPacket_NoRetriesLeft checks that a forwarded packet that
// times out with no retries left is not sent again, writes exactly the fixed
// error acknowledgement for the source packet, and removes the in-flight
// record.
func TestOnTimeoutPacket_NoRetriesLeft(t *testing.T) {
	f := newForwarder(t)
	ctx := f.setup.Initializer.Ctx
	memo := forwardMemo(`,"retries":0`)
	srcPacket := transferPacket(t, senderAddr, hostAddr, memo)
	ack, fwdMemo, forwarded := f.recvPacket(srcPacket)
	require.Nil(t, ack)
	require.True(t, forwarded)

	fwdPacket := f.forwardedPacket(fwdMemo)
	k := f.setup.Keepers.PacketForwardKeeper
	inFlight, err := k.TimeoutShouldRetry(ctx, fwdPacket)
	require.NotNil(t, inFlight, "the forwarded packet must be in flight")
	require.Error(t, err, "no retries are left")

	f.expectRefund(srcPacket, sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	_, retried, err := f.timeoutPacket(fwdPacket)
	require.NoError(t, err)
	require.False(t, retried)

	inFlight, err = k.TimeoutShouldRetry(ctx, fwdPacket)
	require.NoError(t, err)
	require.Nil(t, inFlight, "the in-flight record must be removed")
}
