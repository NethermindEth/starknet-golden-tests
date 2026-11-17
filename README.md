# Starknet Golden Tests

A golden testing framework for validating Starknet JSON-RPC implementations. Compare your RPC endpoint responses against known-good "golden" outputs to ensure compliance and catch regressions.

## Prerequisites

- `jq` — JSON processor
- `curl` — HTTP client
- Go 1.24 or later, for `corpus-diff`
- A Starknet RPC endpoint to test against

## Quick Start

```bash
# Set your RPC endpoint (or use --rpc-url flag)
export STARKNET_RPC=http://localhost:6060

# Run all tests
./golden.sh test

# Run tests for a specific network/folder
./golden.sh test tests/mainnet

# Or use --rpc-url flag directly
./golden.sh test --rpc-url http://localhost:6060
```

## CLI Usage

### Running Tests

```bash
./golden.sh test [--rpc-url <url>] [tests_folder]
```

Runs all golden tests by comparing live RPC responses against expected outputs.

- `--rpc-url` — Your Starknet RPC endpoint (optional if `STARKNET_RPC` env var is set)
- `tests_folder` — Folder containing tests to run (default: auto-detect by chain ID)

Results are saved to a timestamped folder in `results/`. Only failed tests produce diff files.

### Generating Tests

Generate golden test cases from a trusted RPC endpoint:

```bash
# Generate tests for a block (by block number)
./golden.sh generate block [--rpc-url <url>] <block_number>

# Generate tests for a class (by class hash)
./golden.sh generate class [--rpc-url <url>] <class_hash> [block_id]

# Generate tests for a contract (by address)
./golden.sh generate contract [--rpc-url <url>] <contract_address> [block_id]

# Generate tests for a transaction (by hash)
./golden.sh generate transaction [--rpc-url <url>] <transaction_hash>

# Generate getEvents tests derived from a block's events (filters, range, empty result, pagination)
./golden.sh generate events [--rpc-url <url>] <block_number>

# Generate L1->L2 message status tests (by the L1/Ethereum transaction hash that sent the message)
./golden.sh generate messages [--rpc-url <url>] <l1_transaction_hash>

# Generate version/chainId tests
./golden.sh generate version [--rpc-url <url>]
```

**Examples:**

```bash
# Using STARKNET_RPC env var
export STARKNET_RPC=http://localhost:6060
./golden.sh generate block 100
./golden.sh generate class 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f # uses "latest" block
./golden.sh generate class 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f 100 # uses block_number
./golden.sh generate class 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f 0x4223f3e4f2d1e6c9753b04974acdf045e602ccfe784ea6d3722697bda0fc4d2 # uses block_hash
./golden.sh generate contract 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213 # uses "latest" block
./golden.sh generate contract 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213 100 # uses block_number (also generates getNonce)
./golden.sh generate transaction 0x1b4d9f09276629d496af1af8ff00173c11ff146affacb1b5c858d7aa89001ae

# Or using --rpc-url flag
./golden.sh generate block --rpc-url http://localhost:6060 100
```

The generators automatically:
- Create input/output pairs for all relevant RPC methods
- Test both block number and block hash variants
- Verify that different query methods return consistent results

### Regenerating Outputs

Regenerate all golden test outputs from a trusted RPC endpoint running the latest spec version:

```bash
./golden.sh regen [--rpc-url <url>] [tests_folder]
```

- `--rpc-url` — Your Starknet RPC endpoint (optional if `STARKNET_RPC` env var is set)
- `tests_folder` — Folder containing tests to regenerate (default: auto-detect by chain ID)

**Examples:**

```bash
# Regenerate all outputs for auto-detected network
./golden.sh regen --rpc-url http://localhost:6060

# Regenerate outputs for a specific test folder
./golden.sh regen --rpc-url http://localhost:6060 tests/mainnet
```

Use this when you need to update all expected outputs after changes to the RPC implementation. This regenerates the default `.output.json` files (not version-specific variants).

### Creating Version Variants

Create version-specific output variants for older node versions:

```bash
./golden.sh variant [--rpc-url <url>] [tests_folder]
```

- `--rpc-url` — Your Starknet RPC endpoint for the older node version
- `tests_folder` — Folder containing tests (default: auto-detect by chain ID)

**Examples:**

```bash
# Create variants from an older node version
./golden.sh variant --rpc-url http://old-node:6060

# Create variants for a specific test folder
./golden.sh variant --rpc-url http://old-node:6060 tests/mainnet
```

This command:
- Queries `starknet_specVersion` to detect the node's spec version
- Creates variant files (e.g., `100.output.0.8.0.json`) only when outputs differ from the default
- Skips creating variants when the output matches the resolved output for that version

Variant files allow tests to pass against multiple RPC implementation versions.

## corpus-diff

`corpus-diff` is a Go tool that checks responses from a corpus: a JSON file of
JSON-RPC requests with a `meta` header. Juno's `corpus-gen` writes corpora, and
you can also write one by hand. The tool replaces the `jq` comparison of
`golden.sh test` with order-aware rules.

```bash
go build -o corpus-diff ./cmd/corpus-diff

# Check the corpus format
./corpus-diff validate corpora/

# Record the responses of a node
./corpus-diff record --corpus corpora/sepolia --node "$STARKNET_RPC" --out recorded/sepolia/v0.10

# Compare a node with the records (the first source is the reference)
./corpus-diff compare --corpus corpora/sepolia --records recorded/sepolia/v0.10 --node "$STARKNET_RPC" --report report/

# Compare two nodes
./corpus-diff compare --corpus corpora/sepolia --node "$NODE_A" --node "$NODE_B"
```

Exit codes: 0 = all pass, 1 = a check failed or a request has no record, 2 =
usage or setup error (for example, the node is on another chain or spec
version, `--filter` matches no request, or a `--records` path does not exist).
In directory mode, a corpus without its records file under an existing
`--records` directory is not a setup error: its requests fail as not recorded.

### Corpus format

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

### Records

`record` writes one JSON line for each request, sorted by request key, after a
header line (chain id, spec version, corpus digest, node version, head). It
sorts the arrays that the spec defines as sets, so a new recording of the same
data gives the same lines. If a response has a `status` or `finality_status`
that can still change (`ACCEPTED_ON_L2`, `PRE_CONFIRMED`, ...), it refuses the
response, unless you give `--allow-volatile`. Responses without a status (for
example getStateUpdate, getEvents, getStorageAt) are not checked, so record only
blocks that are on L1.

### Comparison rules

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

## How It Works

1. **Test Structure**: Each test is a pair of `.input.json` (RPC request) and `.output.json` (expected response) files.

2. **Normalization**: Before comparison, responses are normalized to ensure consistent ordering and formatting. The `query-rpc.sh` script automatically applies:
   - Default normalization: sort keys, compact JSON
   - Method-specific normalization (if `scripts/normalize/<method>.sh` exists)

3. **Comparison**: The `diff.sh` script compares the live (normalized) response against the golden output using unified diff format.

4. **Results**: Test results are saved to `results/<timestamp>/`. Only failing tests produce `.diff` files containing the differences.

## Development

### Pre-commit

```bash
# Install pre-commit hooks
pre-commit install
```

The project uses pre-commit hooks for shell script quality:

- **shfmt** — Formats shell scripts (4-space indent, case indent, binary ops on new line)
- **shellcheck** — Lints shell scripts for common issues

### Adding Method-Specific Normalization

Some RPC methods return data in non-deterministic order. To handle this, create a normalization script:

```bash
# Create scripts/normalize/<method_name>.sh
#!/bin/bash
# Example: Sort an array by a specific field
script_dir="$(dirname "$0")"
"$script_dir/sort-array.sh" '.result.items' '.id'
```

The script receives JSON on stdin and should output normalized JSON on stdout.

### Test File Naming Convention

Test files follow the pattern: `<identifier>.input.json` / `<identifier>.output.json`

## License

See [LICENSE](LICENSE) for details.
