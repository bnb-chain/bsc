#!/usr/bin/env python3
"""Render BEP-702's storage-layout section from core/vm/testdata/b20_layout.json.

The layout is consensus — a slot number decides the state root — so the spec has
to carry it. A table transcribed by hand into Markdown drifts from the code
silently, which is how the interface mirror ended up with a PolicyType two members
short and the two registry addresses swapped. So the fixture is the single source:
b20_layout_pin_test.go holds it against the Go constants, and this script renders
it into the spec.

Usage:
    python3 scripts/b20-layout-doc.py                    # print the section
    python3 scripts/b20-layout-doc.py --check FILE       # exit 1 if FILE's section differs
    python3 scripts/b20-layout-doc.py --write FILE       # replace the section in FILE

The section is delimited by the two markers below so --check and --write can find
it without parsing the surrounding document.
"""

import argparse
import json
import sys

BEGIN = "<!-- BEGIN GENERATED STORAGE LAYOUT -->"
END = "<!-- END GENERATED STORAGE LAYOUT -->"
DEFAULT_FIXTURE = "core/vm/testdata/b20_layout.json"


def render(layout):
    d = layout["derivation"]
    out = [BEGIN, ""]
    out.append(
        "An N20 operation writes real storage under the token's account, so a slot "
        "number decides that account's storage root and therefore the block hash. "
        "Everything in this section is consensus: two implementations that disagree "
        "about one slot diverge on the first operation that touches it. The tables are "
        "generated from the reference implementation rather than transcribed "
        "(`scripts/b20-layout-doc.py`)."
    )
    out.append("")
    out.append(
        "**Namespaces.** Each domain of state occupies an "
        "[ERC-7201](https://eips.ethereum.org/EIPS/eip-7201) namespace, rooted at "
        f"`{layout['erc7201']}`. The mask gives each namespace 256 consecutive slots and "
        "keeps it clear of the low slots a naive layout would take. An implementation "
        "MUST use these strings verbatim; the roots follow from them and are listed so a "
        "mismatch is caught by inspection."
    )
    out.append("")
    out.append("| Namespace | Held by | Root |")
    out.append("|---|---|---|")
    for ns in layout["namespaces"]:
        out.append(f"| `{ns['name']}` | {ns['held_by']} | `{ns['root']}` |")
    out.append("")

    out.append("**Fields.** Slot numbers are per namespace, and append-only: an "
               "existing field MUST NOT be renumbered, since every stored value would "
               "move with it.")
    out.append("")
    out.append("| Namespace | Slot | Field | Type |")
    out.append("|---|---|---|---|")
    for ns in layout["namespaces"]:
        for i, f in enumerate(ns["fields"]):
            label = f"`{ns['name']}`" if i == 0 else ""
            typ = f["type"].replace("|", "\\|")
            out.append(f"| {label} | {f['slot']} | `{f['name']}` | `{typ}` |")
    out.append("")

    out.append("**Slot derivation.** Identical to Solidity's, so that a reference "
               "contract and a native token reach the same state root for the same "
               "operations:")
    out.append("")
    out.append("```")
    for label, key in [
        ("fixed field", "fixed_field"),
        ("mapping, value key", "mapping_value_key"),
        ("mapping, string key", "mapping_string_key"),
        ("nested mapping", "nested_mapping"),
        ("string, len <= 31", "string_short"),
        ("string, len >= 32", "string_long"),
        ("dynamic array", "dynamic_array"),
    ]:
        out.append(f"{label:<22}{d[key]}")
    out.append("```")
    out.append("")
    out.append(
        "A `string` length word MUST be treated as untrusted: one that no write could "
        f"have produced reads as the empty string, and a length above {d['string_max_len']} "
        "bytes MAY be rejected outright — the slots alone would cost orders of magnitude "
        "more than any block limit, so no such value can exist. An implementation MUST "
        "NOT fault or allocate from an unvalidated length word. A fault inside a "
        "precompile is a node crash rather than a revert, so this is not left to "
        "reachability."
    )
    out.append("")

    out.append("**Packed words.** Lanes are least significant first. A write MUST "
               "rebuild the whole word, leaving no lane of a previous value behind.")
    out.append("")
    out.append("| Word | Lanes | Note |")
    out.append("|---|---|---|")
    for p in layout["packed_words"]:
        parts, reserved = [], False
        for lane in p["lanes"]:
            if lane["name"] == "reserved":
                reserved = True  # implied by the lanes that are named
                continue
            bit, width = lane["bit_offset"], lane["width"]
            at = f"bit {bit}" if width == 1 else f"bits {bit}..{bit + width}"
            parts.append(f"`{lane['name']}` {at}")
        if reserved:
            parts.append("rest reserved")
        out.append(f"| {p['where']} | {', '.join(parts)} | {p.get('note', '')} |")
    out.append("")
    out.append("**Not persisted.** " + "; ".join(layout["not_storage"]) + ".")
    out.append("")
    out.append(END)
    return "\n".join(out) + "\n"


def splice(doc, section):
    if BEGIN not in doc or END not in doc:
        sys.exit(f"the document holds no {BEGIN} / {END} pair; add the markers first")
    head = doc[: doc.index(BEGIN)]
    tail = doc[doc.index(END) + len(END):]
    return head + section.rstrip("\n") + tail


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--fixture", default=DEFAULT_FIXTURE)
    ap.add_argument("--check", metavar="FILE")
    ap.add_argument("--write", metavar="FILE")
    args = ap.parse_args()

    section = render(json.load(open(args.fixture)))
    target = args.check or args.write
    if not target:
        sys.stdout.write(section)
        return
    doc = open(target).read()
    updated = splice(doc, section)
    if args.check:
        if updated != doc:
            sys.exit(f"{target}'s storage section is stale; rerun with --write {target}")
        print(f"{target}: storage section matches {args.fixture}")
        return
    open(target, "w").write(updated)
    print(f"wrote the storage section into {target}")


if __name__ == "__main__":
    main()
