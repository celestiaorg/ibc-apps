package packetforward_test

import (
	"testing"

	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

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
