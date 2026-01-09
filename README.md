# Starknet Golden Tests

A golden testing framework for validating Starknet JSON-RPC implementations. Compare your RPC endpoint responses against known-good "golden" outputs to ensure compliance and catch regressions.

## Prerequisites

- `jq` — JSON processor
- `curl` — HTTP client
- A Starknet RPC endpoint to test against

## Quick Start

```bash
# Run all tests against your RPC endpoint
./golden.sh test http://localhost:6060

# Run tests for a specific network/folder
./golden.sh test http://localhost:6060 tests/mainnet
```

## CLI Usage

### Running Tests

```bash
./golden.sh test <rpc_url> [tests_folder]
```

Runs all golden tests by comparing live RPC responses against expected outputs.

- `rpc_url` — Your Starknet RPC endpoint (required)
- `tests_folder` — Folder containing tests to run (default: `tests`)

Results are saved to a timestamped folder in `results/`. Only failed tests produce diff files.

### Generating Tests

Generate golden test cases from a trusted RPC endpoint:

```bash
# Generate tests for a block (by block number)
./golden.sh generate block <network> <block_number> <rpc_url>

# Generate tests for a transaction (by hash)
./golden.sh generate transaction <network> <transaction_hash> <rpc_url>
```

**Examples:**

```bash
# Generate block 100 tests for mainnet
./golden.sh generate block mainnet 100 http://localhost:6060

# Generate transaction tests
./golden.sh generate transaction mainnet 0x1b4d9f09276629d496af1af8ff00173c11ff146affacb1b5c858d7aa89001ae http://localhost:6060
```

The generators automatically:
- Create input/output pairs for all relevant RPC methods
- Test both block number and block hash variants
- Verify that different query methods return consistent results

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
