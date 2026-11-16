package packetforward_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

// recvWithoutForwarding runs OnRecvPacket for a memo that must be rejected
// before any funds are received: no mock is expected, so a call into the
// transfer app or keeper fails the test.
func recvWithoutForwarding(t *testing.T, memo string) ibcexported.Acknowledgement {
	t.Helper()
	ctl := gomock.NewController(t)
	defer ctl.Finish()
	setup := test.NewTestSetup(t, ctl)
	packet := transferPacket(t, senderAddr, hostAddr, memo)
	var ack ibcexported.Acknowledgement
	require.NotPanics(t, func() {
		ack = setup.ForwardMiddleware.OnRecvPacket(setup.Initializer.Ctx, packet, test.AccAddress())
	})
	return ack
}

// TestOnRecvPacket_ForwardDecodesToNil checks memos that have a non-null
// "forward" key but whose forward metadata decodes to nil, because a later
// key that matches "forward" without case is null. They get an error
// acknowledgement and do not panic.
func TestOnRecvPacket_ForwardDecodesToNil(t *testing.T) {
	fwd := `{"receiver":"` + destAddr + `","port":"transfer","channel":"channel-0"}`
	for _, memo := range []string{
		`{"forward":` + fwd + `,"Forward":null}`,
		`{"forward":` + fwd + `,"FORWARD":null}`,
		`{"forward":` + fwd + `,"Forward":null}`,
		`{"Forward":null,"forward":` + fwd + `,"fORWARD":null}`,
	} {
		ack := recvWithoutForwarding(t, memo)
		require.NotNil(t, ack, memo)
		require.False(t, ack.Success(), memo)
	}
}

// fixedErrorAck is the acknowledgement for every forward memo that
// packet-forward-middleware rejects.
const fixedErrorAck = `{"error":"packet-forward-middleware error: forward failed"}`

// TestOnRecvPacket_ErrorAckIsFixed checks that forward memos rejected for
// different reasons all get the same acknowledgement bytes, with no Go
// error text in them.
func TestOnRecvPacket_ErrorAckIsFixed(t *testing.T) {
	for name, memo := range map[string]string{
		"receiver is a number":     `{"forward":{"receiver":5,"port":"transfer","channel":"channel-0"}}`,
		"invalid port":             `{"forward":{"receiver":"r","port":"x","channel":"channel-0"}}`,
		"invalid channel":          `{"forward":{"receiver":"r","port":"transfer","channel":"!"}}`,
		"empty receiver":           `{"forward":{"port":"transfer","channel":"channel-0"}}`,
		"timeout out of range":     `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":1e19}}`,
		"invalid duration string":  `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":"1e19"}}`,
		"retries over uint8":       `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","retries":256}}`,
		"forward decodes to null":  `{"forward":{"receiver":"r","port":"transfer","channel":"channel-0"},"Forward":null}`,
		"forward is a string":      `{"forward":"x"}`,
		"forward is a number":      `{"forward":5}`,
		"forward is an empty list": `{"forward":[]}`,
	} {
		ack := recvWithoutForwarding(t, memo)
		require.NotNil(t, ack, name)
		require.Equal(t, fixedErrorAck, string(ack.Acknowledgement()), name)
	}
}

// forwarder runs packet-forward-middleware with mocks that accept any
// receive and any transfer, and records the memo of each transfer it sends.
type forwarder struct {
	t     *testing.T
	setup *test.Setup
	memos []string
}

func newForwarder(t *testing.T) *forwarder {
	t.Helper()
	ctl := gomock.NewController(t)
	t.Cleanup(ctl.Finish)
	f := &forwarder{t: t, setup: test.NewTestSetup(t, ctl)}
	m := f.setup.Mocks
	m.IBCModuleMock.EXPECT().OnRecvPacket(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(channeltypes.NewResultAcknowledgement([]byte{1})).AnyTimes()
	m.IBCModuleMock.EXPECT().OnTimeoutPacket(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.TransferKeeperMock.EXPECT().Transfer(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *transfertypes.MsgTransfer) (*transfertypes.MsgTransferResponse, error) {
			f.memos = append(f.memos, msg.Memo)
			return &transfertypes.MsgTransferResponse{Sequence: uint64(len(f.memos))}, nil
		}).AnyTimes()
	return f
}

// recv receives a packet with memo. It returns the acknowledgement and, if
// the packet was forwarded, the memo of the forwarded packet.
func (f *forwarder) recv(memo string) (ibcexported.Acknowledgement, string, bool) {
	f.t.Helper()
	n := len(f.memos)
	ack := f.setup.ForwardMiddleware.OnRecvPacket(f.setup.Initializer.Ctx, transferPacket(f.t, senderAddr, hostAddr, memo), test.AccAddress())
	if len(f.memos) == n {
		return ack, "", false
	}
	return ack, f.memos[n], true
}

// timeout times out the last forwarded packet, sent on channel-0 with
// memo, and returns the memo of the retried packet if it was retried.
func (f *forwarder) timeout(memo string) (string, bool, error) {
	f.t.Helper()
	n := len(f.memos)
	data, err := transfertypes.ModuleCdc.MarshalJSON(&transfertypes.FungibleTokenPacketData{
		Denom: testDenom, Amount: testAmount, Sender: intermediateAddr, Receiver: destAddr, Memo: memo,
	})
	require.NoError(f.t, err)
	packet := channeltypes.Packet{
		Sequence: uint64(n), SourcePort: port, SourceChannel: channel,
		DestinationPort: port, DestinationChannel: "channel-99", Data: data,
	}
	err = f.setup.ForwardMiddleware.OnTimeoutPacket(f.setup.Initializer.Ctx, packet, test.AccAddress())
	if len(f.memos) == n {
		return "", false, err
	}
	return f.memos[n], true, err
}

// forwardMemo returns a forward memo to channel-0 with the given extra
// fields of the forward object (each starting with a comma).
func forwardMemo(fields string) string {
	return `{"forward":{"receiver":"` + destAddr + `","port":"transfer","channel":"channel-0"` + fields + `}}`
}

// TestForwardedMemoIsNextAsSent checks the memo of the forwarded packet for
// each form of forward.next. An object (or other non-string value) is
// forwarded as the sender's bytes: not re-encoded, so whitespace, key order,
// repeated keys and characters such as '<' stay as sent. A string is
// forwarded as its unquoted content, which must be JSON.
func TestForwardedMemoIsNextAsSent(t *testing.T) {
	testCases := []struct {
		name string
		next string // JSON value of forward.next
		want string // forwarded memo
		// fail is true when the packet must get the error ack.
		fail bool
	}{
		{name: "object", next: `{"forward":{"receiver":"r2"}}`, want: `{"forward":{"receiver":"r2"}}`},
		{name: "object with spaces", next: `{ "b" : 1,  "a" : [ 1 , 2 ] }`, want: `{ "b" : 1,  "a" : [ 1 , 2 ] }`},
		{name: "html characters", next: `{"a":"<&>"}`, want: `{"a":"<&>"}`},
		{name: "escapes kept", next: `{"a":"\u003c\ud800"}`, want: `{"a":"\u003c\ud800"}`},
		{name: "repeated key kept", next: `{"a":1,"a":2}`, want: `{"a":1,"a":2}`},
		{name: "number kept", next: `1e20`, want: `1e20`},
		{name: "array", next: `[ 1 ]`, want: `[ 1 ]`},
		{name: "string object", next: strconv.Quote(`{ "a" : "<" }`), want: `{ "a" : "<" }`},
		{name: "string null", next: `"null"`, want: `null`},
		{name: "string string", next: strconv.Quote(`"x"`), want: `"x"`},
		{name: "string not JSON", next: `"hello"`, fail: true},
		{name: "empty string", next: `""`, fail: true},
		{name: "last next wins", next: `{"a":1},"next":{"b":2}`, want: `{"b":2}`},
		{name: "last next wins, other case", next: `"hello","NEXT":{"b":2}`, want: `{"b":2}`},
		{name: "null next", next: `null`, want: ``},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newForwarder(t)
			ack, memo, forwarded := f.recv(forwardMemo(`,"next":` + tc.next))
			if tc.fail {
				require.False(t, forwarded)
				require.Equal(t, fixedErrorAck, string(ack.Acknowledgement()))
				return
			}
			require.Nil(t, ack)
			require.True(t, forwarded)
			require.Equal(t, tc.want, memo)
		})
	}
}
