# Offline path-state recovery

`offline-recover` is an explicit repair tool for a stopped node whose retained
path-state history can restore a known canonical block, but whose execution head
or snap-sync metadata prevents normal recovery. It does not change normal node,
fork-choice, finality, or sync behavior and never starts the node.

Build it with `go build -o build/bin/offline-recover ./cmd/offline-recover`.

## Supported recovery

The tool requires a non-genesis canonical block, its exact hash and state root,
a durable MPT state ID at or after the target, and continuous retained reverse
histories ending at that durable state. A target below the chain freezer, indexed
state/trie histories, legacy freezer migrations and journal-only state are not
supported. Use the same client version and schema as the data being repaired.
Only the default `<datadir>/geth/chaindata`, its `ancient` directory, and journal
files beneath `<datadir>/geth` are supported; custom database/journal locations
must not be used with this command.

The command takes the node instance lock. Inspection opens the database and
freezers read-only, decodes all required history records, and replays every
transition into an in-memory overlay using the native trie recovery algorithm.
Every reconstructed account/storage root is verified along this recovery path.
This is not an exhaustive traversal of all untouched state or contract bytecode.
The in-memory overlay can be large; use a small, deliberate `--max-state-steps`.

```sh
build/bin/offline-recover \
  --datadir "$DATADIR" \
  --target "$BLOCK_NUMBER" \
  --target-hash "$BLOCK_HASH" \
  --target-root "$STATE_ROOT" \
  --max-state-steps "$MAX_REVERSE_TRANSITIONS"
```

There is no default target height. Hashes must be exact 32-byte hex values. Obtain
these values independently from the intended chain; recovery is not a way to
make an arbitrary header's state exist.

## Optional missing-root reconstruction

`--reconstruct-missing-root` allows a missing account-trie root to be reconstructed
as a branch from its surviving first-level children. Its hash must exactly equal
the saved snapshot root, which must also agree with the retained history endpoint.
The entire reverse replay must still pass before any original-data write.
A different child hash, missing required subtree or incompatible trie shape will
be rejected. A stale snapshot root alone is not accepted as evidence of recovery.

Path-based sync can delete an old node at the same path when its hash differs
from the requested new state. The reverse histories may remain intact even if
that root entry is gone. This option addresses that specific recoverable case;
it does not recreate arbitrary missing state.

## Applying recovery

Keep the node stopped. Prefer an independently copied datadir. In the copied
**datadir root**, create `offline-recovery-copy.json` with absolute, resolved paths:

```json
{"source":"/original/node","destination":"/recovery/node","complete":true}
```

The tool checks that source/destination do not contain one another and rejects
symlinks and files hard-linked to the source. The manifest is an operator
attestation that the copy is complete, not a substitute for verifying a backup.
Use the inspection command with the copy's datadir and add `--apply`.

For deliberate repair of the original datadir, add all of:

```sh
--apply --in-place --acknowledge-in-place "$BLOCK_HASH"
```

Apply repeats the in-memory replay check, checks journal quarantine destinations,
and creates `OFFLINE-RECOVERY-IN-PROGRESS` before mutation. It then:

1. Restores the verified root entry, if explicitly requested and needed.
2. Replays reverse histories and verifies the target root and state ID.
3. Syncs the state before truncating the state/trie-node history heads.
4. Renames stale `merkle.journal` files to `.before-offline-recovery`.
5. Removes canonical mappings above the target and associated transaction lookup
   entries, retaining block/header/receipt data. Resets stale pivot, finalized,
   snap-sync, snapshot and derived filter-map metadata, and aligns the execution,
   header and fast-sync heads to the verified target.
6. Writes `OFFLINE-RECOVERY-COMPLETE` only after the resulting markers are checked.

**This is an administrative finality reset.** It is not a consensus-approved
reorganization and is inappropriate on an independently operating production
node without determining the correct chain. Snapshot/derived-index rebuilding
on restart can cause substantial background I/O.

The sequence is not one cross-store atomic transaction. If apply fails or the
process is interrupted, leave the node stopped. Do not delete the in-progress
marker and retry blindly; the database may be partially reverted. There is no
automatic genesis fallback, resume, network reconnection or restart.

## Verification after recovery

Start isolated first, verify the target block hash/root and actual state reads,
then reconnect only to the intended network. Check that execution advances and
that recent canonical hashes agree with trusted peers. A successful recovery
report does not by itself prove that the node has caught up. Wait for background
index/snapshot work to settle before comparing performance measurements.

For path-state snapshots, look for `Generated snapshot` in the node log
(`Generated state snapshot` is used by other snapshot implementations).
`eth_syncing=false` alone does not establish that this background work has
finished. Record sustained head agreement, transaction-pool backlog and disk
I/O after completion, then validate under the intended load. Generation counts
and cursors are not a reliable global percentage; an ETA is only an estimate.
