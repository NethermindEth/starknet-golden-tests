# Starknet Golden Tests

Golden tests for Starknet JSON-RPC nodes. A corpus holds JSON-RPC requests, and
a records file holds the expected response to each request. `corpus-diff`
sends the requests to a node and compares the responses with the records.

## Prerequisites

- Go 1.24 or later
- A Starknet RPC endpoint

## Quick start

```bash
go build -o corpus-diff ./cmd/corpus-diff

./corpus-diff compare --corpus corpora/sepolia/v0.10 \
  --records recorded/sepolia/v0.10 --node "$STARKNET_RPC"
```

Use the folder that matches the chain and the spec version of the node
(`starknet_chainId`, `starknet_specVersion`).

## Layout

```
corpora/<network>/v<version>/<name>.json     requests, one per line, with labels
recorded/<network>/v<version>/<name>.jsonl   the expected responses
```

`handpicked.json` holds the cases that people chose, migrated from the old
`tests/` folder. Corpora that Juno's `corpus-gen` makes go next to it, as more
files.

## Commands

```bash
# Check the corpus format
./corpus-diff validate corpora/

# Compare a node with the records (the first source is the reference)
./corpus-diff compare --corpus corpora/sepolia/v0.10 --records recorded/sepolia/v0.10 \
  --node "$STARKNET_RPC" --report report/

# Compare two nodes
./corpus-diff compare --corpus corpora/sepolia/v0.10 --node "$NODE_A" --node "$NODE_B"

# Record the responses of a trusted node
./corpus-diff record --corpus corpora/sepolia/v0.10 --node "$STARKNET_RPC" \
  --out recorded/sepolia/v0.10
```

Exit codes: 0 = all pass, 1 = a check failed or a request has no record, 2 =
usage or setup error (for example, the node is on another chain or spec
version, `--filter` matches no request, or a `--records` path does not exist).
In directory mode, a corpus without its records file under an existing
`--records` directory fails as not recorded.

`--report <dir>` writes `summary.md` (counts for each method) and
`failures.jsonl` (each failure with its diff paths and both responses).

## Add a test case

1. Add the request to a corpus, on its own line, with the next free `id`. Add
   a label in `meta.labels` and increase `meta.count`.
2. Record only the new request:

   ```bash
   ./corpus-diff record --only-missing --corpus corpora/sepolia/v0.10 \
     --node "$STARKNET_RPC" --out recorded/sepolia/v0.10
   ```

   `--only-missing` keeps the existing records and drops the records of
   requests that are no longer in the corpus.
3. Check the git diff and commit both files.

Use a node that runs a Juno release, and blocks that are on L1. To update all
records after an accepted change of behaviour, record without
`--only-missing` and check the diff.

## Corpus format

```jsonc
{
  "meta": {
    "method": "mixed",                 // one method, or "mixed"
    "rpcVersion": "0.10.3",            // must match the node on major.minor
    "chainId": "0x534e5f5345504f4c4941",
    "count": 2,                        // number of entries in "requests"
    "batch": 0,                        // 0 = plain requests, N = arrays of N requests
    "expectErrors": false,             // true: the requests return errors on purpose
    "labels": { "1": "getBlockWithTxs/100" },  // optional: id -> readable name
    "equivalent": [[1, 2]]             // optional: ids that must return the same response
  },
  "requests": [
    {"jsonrpc": "2.0", "id": 1, "method": "starknet_getBlockWithTxs", "params": {"block_id": {"block_number": 100}}},
    {"jsonrpc": "2.0", "id": 2, "method": "starknet_getBlockWithTxs", "params": {"block_id": {"block_hash": "0x1b2a..."}}}
  ]
}
```

A request is identified by its method and canonical params, not by its id. So
you can add, remove or renumber requests without a change to the other records.

## Records

`record` writes one JSON line for each request, sorted by request key, after a
header line (chain id, spec version, corpus digest, node version, head). It
sorts the arrays that the spec defines as sets, so a new recording of the same
data gives the same lines. If a response has a `status` or `finality_status`
that can still change (`ACCEPTED_ON_L2`, `PRE_CONFIRMED`, ...), it refuses the
response, unless you give `--allow-volatile`. Responses without a status (for
example getStateUpdate, getEvents, getStorageAt) are not checked, so record only
blocks that are on L1.

## Comparison rules

- Felts and numbers are compared as text: `0x1` and `0x01` are different.
- Arrays are compared in order, except the arrays that the spec defines as sets
  or mappings: state diff arrays and `initial_reads`. These are sorted on both
  sides first.
- Errors: `code` and `message` must be equal. `data` is compared only with
  `--strict-error-data`.
- `starknet_specVersion` is compared on major.minor.
- The getEvents `continuation_token` is compared only for presence.
- Requests that depend on the chain head (`latest`, `pre_confirmed`,
  `l1_accepted`, `blockNumber`, `syncing`, `getStorageProof`, getEvents without
  `to_block`) are not recorded and are reported as skipped.

In a failure inside a sorted array, the reported index is the index after the
sort, not the index in the node response.

## License

See [LICENSE](LICENSE) for details.
