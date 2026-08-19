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
    out = [BEGIN, ""]
    out.append(
        "Storage layout is **consensus**, not an implementation detail: an N20 "
        "operation writes real storage under the token's account, so a slot number "
        "decides that account's storage root and therefore the block hash. Two "
        "implementations that disagree about one slot diverge on the first operation "
        "that touches it. Everything in this section is therefore normative, and "
        "generated from the reference implementation rather than transcribed "
        "(`scripts/b20-layout-doc.py`)."
    )
    out.append("")
    out.append(
        "**Namespaces.** Each domain of state occupies an "
        "[ERC-7201](https://eips.ethereum.org/EIPS/eip-7201) namespace, rooted at"
    )
    out.append("")
    out.append(f"```\n{layout['erc7201']}\n```")
    out.append("")
    out.append(
        "The mask leaves each namespace 256 consecutive slots and keeps it clear of "
        "the low slots a naive layout would occupy. An implementation MUST use these "
        "namespace strings verbatim; the roots below follow from them and are given "
        "so that a mismatch is caught by inspection."
    )
    out.append("")
    out.append("| Namespace | Held by | Root |")
    out.append("|---|---|---|")
    for ns in layout["namespaces"]:
        out.append(f"| `{ns['name']}` | {ns['held_by']} | `{ns['root']}` |")
    out.append("")

    for ns in layout["namespaces"]:
        out.append(f"**`{ns['name']}`** — {ns['held_by']}.")
        out.append("")
        out.append("| Slot | Field | Type |")
        out.append("|---|---|---|")
        for f in ns["fields"]:
            out.append(f"| {f['slot']} | `{f['name']}` | `{f['type']}` |")
        out.append("")

    d = layout["derivation"]
    out.append(
        "**Slot derivation.** Identical to Solidity's, so that a reference contract "
        "and a native token produce the same state root for the same operations:"
    )
    out.append("")
    for key, label in [
        ("fixed_field", "Fixed field"),
        ("mapping_value_key", "Mapping, value-typed key"),
        ("mapping_string_key", "Mapping, `string` key"),
        ("nested_mapping", "Nested mapping"),
        ("dynamic_array", "Dynamic array"),
        ("string_short", "String, 31 bytes or fewer"),
        ("string_long", "String, 32 bytes or more"),
    ]:
        out.append(f"- **{label}** — {d[key]}")
    out.append("")
    out.append(
        f"A `string` length word MUST be treated as untrusted: {d['string_malformed']}. "
        f"A value longer than {d['string_max_len']} bytes cannot be created — the slots "
        "alone would cost orders of magnitude more than any block limit — so an "
        "implementation MAY reject a longer length outright, and MUST NOT fault or "
        "allocate from it. A fault inside a precompile is a node crash rather than a "
        "revert, so this is not left to reachability."
    )
    out.append("")

    out.append(
        "**Packed words.** Lanes are least significant first, so lane *n* of a word "
        "occupies bits `[64n, 64n+64)` unless stated otherwise. A write MUST rebuild "
        "the whole word, leaving no lane from a previous value behind."
    )
    out.append("")
    for p in layout["packed_words"]:
        out.append(f"- **{p['where']}**")
        for lane in p["lanes"]:
            if "byte_offset" in lane:
                out.append(f"  - byte {lane['byte_offset']}: `{lane['name']}`")
            else:
                bit, width = lane["bit_offset"], lane["width"]
                span = f"bit {bit}" if width == 1 else f"bits [{bit}, {bit + width})"
                out.append(f"  - {span}: `{lane['name']}`")
        if p.get("note"):
            out.append(f"  - {p['note']}")
    out.append("")

    out.append(
        "**Not storage.** State an implementation MUST NOT persist, because "
        "persisting it changes behaviour:"
    )
    out.append("")
    for item in layout["not_storage"]:
        out.append(f"- {item}")
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
