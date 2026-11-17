package types

// Event emitted for each error acknowledgement that packet-forward-middleware
// creates. The acknowledgement holds fixed text, because ibc-go commits its
// hash; the event carries the cause instead. Events are not part of any
// consensus hash, so the cause can be error text that differs between builds.
//
// For an error acknowledgement returned from OnRecvPacket, ibc-go emits the
// event with "ibccallbackerror-" in front of the event type and of each
// attribute key, as it does for every event of a failed receive.
const (
	EventTypeForwardError = "packet_forward_error"
	AttributeKeyError     = "error"
)
