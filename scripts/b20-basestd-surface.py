#!/usr/bin/env python3
"""Regenerate core/vm/testdata/basestd_surface.json from base-std's interfaces.

base-std (github.com/base/base-std, MIT) is the reference implementation's
published ABI. This fetches its Solidity interfaces at a pinned commit, extracts
every function / event / error signature, and writes them out so the Go side can
diff our registered surface against it without parsing Solidity.

Signatures only, no selectors: hashing them here would need a keccak dependency
and would derive the same values from the same strings, proving nothing. The Go
side hashes them, and that derivation is anchored separately by
TestB20PublishedValuesMatchBaseStd, which pins selector literals base-std publishes in
its own changelog tables.

The fork column is what makes the diff actionable: a signature base-std has and
we do not is a bug if it is Beryl, and a tracked gap if it is Cobalt. Cobalt
membership comes from base-std's own changelog tables, transcribed in COBALT
below — re-check it when bumping the pin.

Usage:
    python3 scripts/b20-basestd-surface.py [--commit SHA] [--out PATH]

Needs only network access to raw.githubusercontent.com. No credentials, no packages.
"""

import argparse
import json
import re
import sys
import urllib.request

REPO = "base/base-std"

# Pinned so a regeneration is reproducible and the diff cannot shift under us
# because base-std moved. Bump deliberately, and re-check COBALT when you do.
DEFAULT_COMMIT = "e30b34212689186b27acd3d340ba0d75d31495e9"

INTERFACES = [
    "src/interfaces/IB20.sol",
    "src/interfaces/IB20Asset.sol",
    "src/interfaces/IB20Factory.sol",
    "src/interfaces/IB20Stablecoin.sol",
    "src/interfaces/IPolicyRegistry.sol",
    "src/interfaces/IActivationRegistry.sol",
    "src/interfaces/IERC8056.sol",
    "src/interfaces/IERC165.sol",
]

# Signatures base-std added at the Cobalt hard fork, transcribed from its
# changelog tables (changelog/02_Cobalt_*.md). Everything else in the interfaces
# is Beryl, which ships on Base today.
COBALT = {
    # ERC-8056 scheduled multiplier (02_Cobalt_B20Asset_multiplier.md)
    "uiMultiplier()",
    "toUIAmount(uint256)",
    "fromUIAmount(uint256)",
    "balanceOfUI(address)",
    "totalSupplyUI()",
    "newUIMultiplier()",
    "effectiveAt()",
    "updateUIMultiplier(uint256,uint256)",
    "cancelUIMultiplierUpdate()",
    "MAX_UI_MULTIPLIER()",
    "supportsInterface(bytes4)",
    "UIMultiplierUpdated(uint256,uint256,uint256)",
    "UIMultiplierUpdateCancelled(uint256,uint256)",
    "EffectiveAtInPast(uint256)",
    "EffectiveAtTooFar(uint256)",
    "UIMultiplierUpdateExists(uint256)",
    "UIMultiplierUpdateDoesNotExist()",
    # Composite policies (02_Cobalt_PolicyRegistry_composite_policy.md)
    "createCompositePolicy(address,uint8,uint64[])",
    "updateComposite(uint64,uint64[])",
    "compositePolicyChildIds(uint64)",
    "MIN_COMPOSITE_CHILD_POLICIES()",
    "MAX_COMPOSITE_CHILD_POLICIES()",
    "CompositePolicyUpdated(uint64,address,uint64[])",
    "InvalidChildPolicy(uint64)",
    "ChildPoliciesOutsideOfRange()",
    # Transfer-based freeze-and-seize (02_Cobalt_B20_seize.md). We implement this
    # one, unlike the other two Cobalt groups — it replaces the deprecated
    # burn-based path, which we skipped.
    "seizeWithMemo(address,address,uint256,bytes32)",
    "SEIZE_ROLE()",
    "SEIZE_HOLDER_POLICY()",
    "SEIZE_RECEIVER_POLICY()",
    "Seized(address,address,address,uint256)",
    "AccountNotSeizable(address)",
}

# Enums and user-defined value types lower to their ABI encoding.
ENUMS = {
    "Variant": "uint8",
    "B20Variant": "uint8",
    "PolicyType": "uint8",
    "PausableFeature": "uint8",
}


def fetch(commit, path):
    url = f"https://raw.githubusercontent.com/{REPO}/{commit}/{path}"
    with urllib.request.urlopen(url, timeout=30) as r:
        return r.read().decode()


def strip_comments(src):
    src = re.sub(r"/\*.*?\*/", "", src, flags=re.S)
    return re.sub(r"//[^\n]*", "", src)


def lower_type(t):
    m = re.match(r"([A-Za-z0-9_.]+)(.*)", t)
    name, suffix = m.group(1).split(".")[-1], m.group(2)
    return ENUMS.get(name, name) + suffix


def arg_types(raw):
    out = []
    for arg in raw.split(","):
        arg = re.sub(r"\b(calldata|memory|storage|indexed|payable)\b", "", arg.strip()).strip()
        if arg:
            out.append(lower_type(arg.split()[0]))
    return out


def extract(src):
    """Return {kind: set(signature)} for one interface file."""
    found = {"function": set(), "event": set(), "error": set()}
    for kind in found:
        pattern = (
            kind + r"\s+(\w+)\s*\(([^;{]*?)\)\s*"
            r"(?:external|internal|public|private|view|pure|returns|;|\{)"
        )
        for m in re.finditer(pattern, src, flags=re.S):
            found[kind].add(f"{m.group(1)}({','.join(arg_types(m.group(2)))})")
    return found


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--commit", default=DEFAULT_COMMIT)
    ap.add_argument("--out", default="core/vm/testdata/basestd_surface.json")
    args = ap.parse_args()

    merged = {"function": set(), "event": set(), "error": set()}
    for path in INTERFACES:
        for kind, sigs in extract(strip_comments(fetch(args.commit, path))).items():
            merged[kind] |= sigs

    unknown = COBALT - (merged["function"] | merged["event"] | merged["error"])
    if unknown:
        sys.exit(f"COBALT names signatures absent from the interfaces: {sorted(unknown)}")

    def entries(kind):
        return [
            {"sig": sig, "fork": "cobalt" if sig in COBALT else "beryl"}
            for sig in sorted(merged[kind])
        ]

    doc = {
        "_comment": (
            "Generated by scripts/b20-basestd-surface.py — do not hand-edit. "
            "base-std is the B20 reference implementation's published ABI (MIT)."
        ),
        "source": f"github.com/{REPO}",
        "commit": args.commit,
        "functions": entries("function"),
        "events": entries("event"),
        "errors": entries("error"),
    }
    with open(args.out, "w") as f:
        json.dump(doc, f, indent=2)
        f.write("\n")
    counts = {k: len(merged[k]) for k in merged}
    cobalt = sum(1 for k in merged for s in merged[k] if s in COBALT)
    print(f"wrote {args.out}: {counts}, {cobalt} of them cobalt")


if __name__ == "__main__":
    main()
