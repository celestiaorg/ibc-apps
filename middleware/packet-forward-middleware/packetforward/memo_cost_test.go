package packetforward_test

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

// maxMemoLength is ibc-go's limit on the memo of a MsgTransfer
// (transfertypes.MaximumMemoLength). A packet from another chain can carry a
// longer memo.
const maxMemoLength = 32768

// padForward pads a forward memo built by build(pad) to n bytes with the
// receiver-like padding string pad.
func padTo(n int, build func(pad string) string) string {
	base := build("")
	if len(base) >= n {
		return base
	}
	return build(strings.Repeat("r", n-len(base)))
}

// nested returns depth levels of {"a": around inner.
func nested(depth int, inner string) string {
	return strings.Repeat(`{"a":`, depth) + inner + strings.Repeat(`}`, depth)
}

// attackMemos returns the forward memos found in review of the
// packet-forward-middleware memo handling, each with the extra forward
// fields given, at most n bytes long unless stated.
func attackMemos(fields string, n int) map[string]string {
	pre := `{"forward":{"receiver":"` + destAddr + `","port":"transfer","channel":"channel-0"` + fields
	var keys strings.Builder
	for i := 0; i < 2000; i++ {
		if i > 0 {
			keys.WriteByte(',')
		}
		keys.WriteString(`"k` + strconv.Itoa(i) + `":1`)
	}
	repeat := func(s string, prefix, suffix string) string {
		return prefix + strings.Repeat(s, max(0, (n-len(prefix)-len(suffix))/len(s))) + suffix
	}
	return map[string]string{
		"2000 keys then repeated next (in next)": repeat(`,"next":{}`, pre+`,"next":{`+keys.String(), `}}}`),
		"2000 keys then repeated next (forward)": repeat(`,"next":{}`, pre+`,"next":{`+keys.String()+`}`, `}}`),
		"repeated Forward objects":               repeat(`},"Forward":{"next":{}`, pre+`,"next":{`+keys.String()+`}`, `}}`),
		"same 1500 keys twice in next": func() string {
			var b strings.Builder
			for r := 0; r < 2; r++ {
				for i := 0; i < 1500; i++ {
					if r > 0 || i > 0 {
						b.WriteByte(',')
					}
					b.WriteString(`"` + strconv.Itoa(i) + `":0`)
				}
			}
			return pre + `,"next":{` + b.String() + `}}}`
		}(),
		"object next, 4000 levels":            pre + `,"next":` + nested(4000, `1`) + `}}`,
		"string next hiding 3000 levels":      pre + `,"next":` + strconv.Quote(nested(3000, `1`)) + `}}`,
		"string next hiding 10001 arrays":     pre + `,"next":"` + strings.Repeat(`[`, 10001) + strings.Repeat(`]`, 10001) + `"}}`,
		"depth 32, numbers 1e20":              repeat(`,1e20`, pre+`,"next":`+strings.Repeat(`{"a":`, 29)+`[1`, `]`+strings.Repeat(`}`, 29)+`}}`),
		"depth 16, numbers 1e20":              repeat(`,1e20`, pre+`,"next":`+strings.Repeat(`{"a":`, 13)+`[1`, `]`+strings.Repeat(`}`, 13)+`}}`),
		"depth 16, < characters":              repeat(`<`, pre+`,"next":`+strings.Repeat(`{"a":`, 13)+`"`, `"`+strings.Repeat(`}`, 13)+`}}`),
		"depth 32, < characters":              repeat(`<`, pre+`,"next":`+strings.Repeat(`{"a":`, 29)+`"`, `"`+strings.Repeat(`}`, 29)+`}}`),
		"depth 16, U+2028 characters":         repeat(" ", pre+`,"next":`+strings.Repeat(`{"a":`, 13)+`"`, `"`+strings.Repeat(`}`, 13)+`}}`),
		"string next null":                    pre + `,"next":"null"}}`,
		"string next not JSON":                pre + `,"next":"hello"}}`,
		"timeout 1e19":                        pre + `,"timeout":1e19}}`,
		"forward decodes to null":             pre + `},"Forward":null}`,
		"reference: plain memo, no forward":   padTo(n, func(p string) string { return `{"a":"` + p + `"}` }),
		"honest 2-hop":                        pre + `,"next":{"forward":{"receiver":"r2","port":"transfer","channel":"channel-1"}}}}`,
		"padded 1-hop at the length":          padTo(n, func(p string) string { return pre + `,"next":{"a":"` + p + `"}}}` }),
		"padded 1-hop, 2000 levels":           pre + `,"next":` + nested(2000, `"`+strings.Repeat("r", max(0, n-len(pre)-12000))+`"`) + `}}`,
		"4000 levels, string next 3000 inner": pre + `,"next":` + strconv.Quote(nested(4000, `1`)) + `}}`,
	}
}

// cost runs fn reps times and returns the best time and the mean bytes
// allocated per run.
func cost(reps int, fn func()) (time.Duration, uint64) {
	best := time.Duration(1 << 62)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < reps; i++ {
		start := time.Now()
		fn()
		best = min(best, time.Since(start))
	}
	runtime.ReadMemStats(&after)
	return best, (after.TotalAlloc - before.TotalAlloc) / uint64(reps)
}

// TestMemoCostIsLinear measures, for each attack memo, the time and the
// allocation of the first hop (OnRecvPacket) and of one retry
// (OnTimeoutPacket) through packet-forward-middleware with mocked keepers.
// It checks that the allocation is linear in the memo length, with a small
// factor, also for memos far longer than ibc-go's 32 KiB MsgTransfer limit.
func TestMemoCostIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	const reps = 5
	// allocBound is the allocation allowed for a packet with data of n
	// bytes: a constant for the mocks and the keeper, and a linear term.
	allocBound := func(n int) uint64 { return 64<<10 + 100*uint64(n) }

	for _, n := range []int{maxMemoLength, 32 * maxMemoLength} {
		for name, memo := range attackMemos(`,"retries":255,"timeout":"1ns"`, n) {
			f := newForwarder(t)
			var fwdMemo string
			var forwarded bool
			packet := transferPacket(t, senderAddr, hostAddr, memo)
			recvTime, recvAlloc := cost(reps, func() { _, fwdMemo, forwarded = f.recvPacket(packet) })
			line := fmt.Sprintf("%-40s memo %-8d data %-8d first hop %-10v %8d B", name, len(memo), len(packet.Data), recvTime, recvAlloc)
			require.LessOrEqual(t, recvAlloc, allocBound(len(packet.Data)), name)
			if forwarded {
				// The in-flight record is keyed by the sequence of the last
				// transfer, which each retry advances.
				fwdPacket := f.forwardedPacket(fwdMemo)
				fwdPackets := make([]channeltypes.Packet, reps)
				for i := range fwdPackets {
					fwdPackets[i] = fwdPacket
					fwdPackets[i].Sequence += uint64(i)
				}
				i := 0
				var retried string
				retryTime, retryAlloc := cost(reps, func() {
					var ok bool
					var err error
					retried, ok, err = f.timeoutPacket(fwdPackets[i])
					i++
					require.NoError(t, err)
					require.True(t, ok)
				})
				require.Equal(t, fwdMemo, retried)
				line += fmt.Sprintf(" | fwd memo %-8d retry %-10v %8d B", len(fwdMemo), retryTime, retryAlloc)
				// The in-flight record holds the data of the original packet.
				require.LessOrEqual(t, retryAlloc, allocBound(len(packet.Data)+len(fwdPacket.Data)), name)
			} else {
				line += " | not forwarded"
			}
			t.Log(line)
		}
	}
}

// TestRepeatedKeysAreLinear checks that the cost of repeated next keys grows
// linearly with their count: doubling the count at most about doubles the
// allocation.
func TestRepeatedKeysAreLinear(t *testing.T) {
	f := newForwarder(t)
	memo := func(n int) string {
		return forwardMemo(`,"next":{"a":1}` + strings.Repeat(`,"next":{"b":[1,2,3]}`, n))
	}
	var prev uint64
	for _, n := range []int{1000, 2000, 4000, 8000, 16000} {
		packet := transferPacket(t, senderAddr, hostAddr, memo(n))
		_, alloc := cost(3, func() { f.recvPacket(packet) })
		t.Logf("%5d repeated next keys, len %7d: %8d B", n, len(memo(n)), alloc)
		if prev > 0 {
			require.Less(t, alloc, 3*prev, "allocation must grow linearly")
		}
		prev = alloc
	}
}
