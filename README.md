# celestiaorg/ibc-apps

This is Celestia's fork of [cosmos/ibc-apps](https://github.com/cosmos/ibc-apps). [celestia-app](https://github.com/celestiaorg/celestia-app) uses its packet-forward-middleware (PFM) through a `replace` directive in its `go.mod`.

For general documentation of the apps and middleware in this repository, see the upstream repository.

## Why fork?

- celestia-app uses ibc-go v8 and PFM v8. Upstream has deprecated this repository and moved PFM into ibc-go v10 and later, so it does not ship new fixes to PFM v8.
- Celestia needs PFM to give the same result on every node and to do a bounded amount of work per packet. The fixes for that change state transitions, so Celestia releases them as fork versions that celestia-app activates at a coordinated network upgrade.

## Maintained branches

| Branch | Tags | Used by |
|--------|------|---------|
| [`celestia/pfm-v8.2.x`](https://github.com/celestiaorg/ibc-apps/tree/celestia/pfm-v8.2.x) | `middleware/packet-forward-middleware/v8.2.1-celestia` | celestia-app `main` |
| `main` | none | Follows upstream `main`. Only CI and docs changes. |

Earlier celestia-app versions use upstream PFM (`v6.1.2` or `v8.2.0`) without this fork.

## Differences from upstream

### `celestia/pfm-v8.2.x`

Based on upstream `middleware/packet-forward-middleware/v8.2.0`. All changes are in [#3](https://github.com/celestiaorg/ibc-apps/pull/3) and are consensus-breaking.

- **Deterministic timeouts**: a numeric timeout outside the int64 range is rejected. Before, the float-to-int conversion gave different results on amd64 and arm64.
- **Deterministic error acknowledgements**: every PFM error acknowledgement has the same fixed text. The Go error text goes in a `packet_forward_error` event instead.
- **Memos forwarded as sent**: `forward.next` and retried memos are passed on as bytes, not decoded and encoded again.
- **Bounded memos**: a forward memo longer than 32768 bytes (the ibc-go `MsgTransfer` memo limit) gets an error acknowledgement before any work is done.
- **No nil dereference**: forward metadata that decodes to null gets an error acknowledgement instead of a panic.

### `main`

- CI fixes and a review request workflow. No Go code changes.

## Contributing

- Open PRs against the branch that celestia-app uses.
- PR titles use [conventional commits](https://www.conventionalcommits.org/) with a review level tag (`[low]`, `[medium]` or `[high]`). Add `!` for a consensus-breaking change.
- A consensus-breaking change needs a new fork version. Do not add it to a tag that a released celestia-app version already uses.
