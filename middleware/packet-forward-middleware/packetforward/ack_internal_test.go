package packetforward

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// TestNewErrorAcknowledgementIsFixed checks that the error acknowledgement
// holds no error text. ibc-go commits the hash of the acknowledgement, and
// error text can differ between builds (source paths, Go versions).
func TestNewErrorAcknowledgementIsFixed(t *testing.T) {
	const want = `{"error":"packet-forward-middleware error: forward failed"}`
	for _, err := range []error{
		errors.New("a"),
		fmt.Errorf("error parsing forward metadata: %w", errors.New("json: cannot unmarshal number into Go struct field")),
		errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "/home/builder/go/pkg/mod/x.go:41"),
		nil,
	} {
		ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
		ack := newErrorAcknowledgement(ctx, "transfer", "channel-0", 1, err)
		require.False(t, ack.Success())
		require.Equal(t, want, string(ack.Acknowledgement()))
		require.Len(t, ctx.EventManager().Events(), 1)
	}
}
