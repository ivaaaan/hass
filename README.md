# hass

Small Go CLI that prints the Hyperliquid asset id for a perp or spot market.

It calls Hyperliquid's `POST /info` endpoint and resolves the id from live metadata.

## Install

```sh
go install github.com/ivaaaan/hass@latest
```

Make sure your Go bin directory is on `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Build from source

```sh
go build -o hass .
```

## Usage

```sh
./hass [--testnet] [--spot] SYMBOL
```

Examples:

```sh
# Mainnet perp. BTC is index 0 in mainnet perp metadata.
./hass BTC

# Mainnet HIP-3 builder-deployed perp. The `xyz` prefix is sent as the meta dex.
./hass xyz:XYZ100

# Testnet perp.
./hass --testnet BTC

# Mainnet spot HYPE/USDC. A bare spot token defaults to the USDC quote.
./hass --spot HYPE

# Testnet spot HYPE/USDC.
./hass --spot --testnet HYPE
```

Output is only the integer asset id, suitable for scripts.

## Resolution rules

### Perps

- `SYMBOL` without a prefix uses default perp metadata (`type: "meta"`) and returns the matching universe index.
- HIP-3 / builder-deployed perps use `DEX:COIN`. The dex prefix is sent as `type: "meta", dex: "DEX"`; the tool finds `COIN`, then calculates:

```text
100000 + perp_dex_index * 10000 + index_in_meta
```

The `perp_dex_index` is resolved from `type: "perpDexs"`.

### Spot

Use `--spot` for spot markets.

- `BASE` defaults to `BASE/USDC`.
- `BASE/QUOTE` looks up that exact token pair.
- Universe names such as `@107` are also accepted.

Spot asset ids are calculated as:

```text
10000 + spotInfo.index
```

Note that this is not the token id. For example, HYPE's spot metadata index is different from HYPE's token index.
