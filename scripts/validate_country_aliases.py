#!/usr/bin/env python3
import json
import re
import sys
import unicodedata
from collections import defaultdict
from pathlib import Path


ALIASES_PATH = Path("firecrawl_toolkit/data/country_aliases.json")
REQUIRED_CODES = {"AX", "BQ", "CW"}


def normalize(text: str) -> str:
    if not text:
        return ""
    s = unicodedata.normalize("NFKD", text)
    s = "".join(ch for ch in s if not unicodedata.combining(ch))
    s = s.replace("\u3000", " ")
    s = s.strip().lower()
    s = re.sub(r"[^\w\s'-]", " ", s, flags=re.UNICODE)
    s = re.sub(r"[_\s]+", " ", s).strip()
    return s


def generate_variants(alias: str) -> set[str]:
    variants = set()
    base = alias.strip()
    variants.add(base)

    n = normalize(base)
    variants.add(n)
    variants.add(re.sub(r"[^\w\s]", "", n))

    if "," in base:
        parts = [p.strip() for p in base.split(",") if p.strip()]
        if len(parts) >= 2:
            reordered = " ".join(reversed(parts))
            variants.add(reordered)
            variants.add(normalize(reordered))

    parts = n.split()
    if len(parts) == 2:
        variants.add(" ".join(reversed(parts)))

    return {v for v in variants if v}


def main() -> int:
    data = json.loads(ALIASES_PATH.read_text(encoding="utf-8"))

    duplicate_issues: list[str] = []
    key_to_codes: dict[str, set[str]] = defaultdict(set)
    for code, names in data.items():
        if not isinstance(names, list):
            duplicate_issues.append(f"{code}: alias list is not a list")
            continue

        seen = set()
        for alias in names:
            if not isinstance(alias, str):
                duplicate_issues.append(f"{code}: non-string alias {alias!r}")
                continue
            if alias in seen:
                duplicate_issues.append(f"{code}: duplicate alias {alias!r}")
            seen.add(alias)

            for variant in generate_variants(alias):
                key = normalize(variant)
                if key:
                    key_to_codes[key].add(code)

    missing_codes = sorted(code for code in REQUIRED_CODES if code not in data)
    conflicts = sorted((k, sorted(v)) for k, v in key_to_codes.items() if len(v) > 1)

    if duplicate_issues:
        print("[ERROR] duplicate/non-string alias issues:")
        for issue in duplicate_issues:
            print(f"  - {issue}")

    if missing_codes:
        print("[ERROR] missing required country/area codes:")
        for code in missing_codes:
            print(f"  - {code}")

    if conflicts:
        print("[ERROR] normalized alias conflicts across multiple codes:")
        for key, codes in conflicts[:50]:
            print(f"  - {key!r}: {codes}")
        if len(conflicts) > 50:
            print(f"  ... and {len(conflicts) - 50} more")

    if duplicate_issues or missing_codes or conflicts:
        return 1

    print(
        f"[OK] aliases valid: {len(data)} codes, "
        f"{sum(len(v) for v in data.values() if isinstance(v, list))} aliases"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
