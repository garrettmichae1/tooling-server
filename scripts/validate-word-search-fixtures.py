#!/usr/bin/env python3
"""Independently check fixed server fixtures before a native client is added.

Uses only Python's standard library: no generator, Go/TypeScript implementation,
inference, browser, or network. This validates mechanics and source binding, not
whether the supplied words are true, age appropriate, or relevant to a theme.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import math
from decimal import Decimal
from html.parser import HTMLParser
from pathlib import Path
import re
import struct
import sys


VERSION = "wordsearch-v1"
REQUEST_LIMIT = 16_384
RESPONSE_LIMIT = 262_144
HTML_LIMIT = 98_304
GO_SPACE = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
DIRECTIONS = {
    "E": (0, 1), "S": (1, 0), "SE": (1, 1), "NE": (-1, 1),
    "W": (0, -1), "N": (-1, 0), "NW": (-1, -1), "SW": (1, -1),
}
ALLOWED = {"easy": ["E", "S"], "medium": ["E", "S", "SE", "NE"], "hard": list(DIRECTIONS)}
RESULT_KEYS = {
    "schema_version", "generator_version", "puzzle_id", "title", "difficulty",
    "size", "seed", "directions", "grid", "words", "placements", "filename",
    "media_type", "html", "answer_key_html", "offline", "network_requests",
    "preview_requires_scripts", "puzzle_verified", "content_verified",
}


class InvalidFixture(ValueError):
    """A fixed failure with no supplied content in its text."""


def require(condition: bool, code: str) -> None:
    if not condition:
        raise InvalidFixture(code)


def object_keys(value: object, expected: set[str], code: str) -> dict:
    require(type(value) is dict and set(value) == expected, code)
    return value


def integer(value: object, minimum: int, maximum: int, code: str) -> int:
    # The server contract uses JSON's binary64 number decoding in Go and JS.
    # Integral decoded numbers include 8.0/8e0; Decimal retains the raw fixture
    # token until this explicit conversion, rather than changing the protocol.
    if type(value) is Decimal:
        number = float(value)
        require(math.isfinite(number) and number.is_integer(), code)
        value = int(number)
    require(type(value) is int and minimum <= value <= maximum, code)
    return value


def utf8(value: object, maximum: int, code: str) -> str:
    require(type(value) is str and "\0" not in value, code)
    try:
        length = len(value.encode("utf-8", errors="strict"))
    except UnicodeError:
        raise InvalidFixture(code) from None
    require(length <= maximum, code)
    return value


def strict_pairs(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate_json_key")
        result[key] = value
    return result


def strict_shape(value: object, depth: int = 0) -> None:
    require(depth <= 32, "json_depth")
    if type(value) is dict:
        for key, child in value.items():
            utf8(key, RESPONSE_LIMIT, "invalid_json_string")
            strict_shape(child, depth + 1)
    elif type(value) is list:
        for child in value:
            strict_shape(child, depth + 1)
    elif type(value) is str:
        utf8(value, RESPONSE_LIMIT, "invalid_json_string")


def load_json(raw: bytes) -> object:
    def invalid_constant(_: str) -> None:
        raise InvalidFixture("invalid_json_number")

    try:
        value = json.loads(raw.decode("utf-8", errors="strict"),
                           object_pairs_hook=strict_pairs, parse_float=Decimal,
                           parse_constant=invalid_constant)
        strict_shape(value)
        return value
    except (UnicodeError, json.JSONDecodeError, RecursionError):
        raise InvalidFixture("invalid_json") from None


def json_bytes(value: object) -> bytes:
    def decimal_json(number: Decimal) -> int:
        return integer(number, 0, 2_147_483_647, "invalid_json_integer")

    # Go encoding/json escapes these characters before applying its byte cap.
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"),
                         allow_nan=False, default=decimal_json)
    for original, escaped in (("&", "\\u0026"), ("<", "\\u003c"), (">", "\\u003e"),
                              ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        encoded = encoded.replace(original, escaped)
    return encoded.encode("utf-8")


def digest(fields: list[str]) -> bytes:
    value = hashlib.sha256()
    for field in fields:
        encoded = field.encode("utf-8")
        value.update(struct.pack(">I", len(encoded)))
        value.update(encoded)
    return value.digest()


def canonical_call(value: object) -> dict:
    call = object_keys(value, {"tool", "arguments"}, "call_fields")
    require(call["tool"] == "make_word_search", "call_tool")
    args = call["arguments"]
    require(type(args) is dict and {"title", "words", "difficulty"} <= set(args)
            and set(args) <= {"title", "words", "difficulty", "size", "seed"}, "argument_fields")
    title = utf8(args["title"], 120, "title").strip(GO_SPACE)
    require(bool(title), "title")
    require(type(args["difficulty"]) is str and args["difficulty"] in ALLOWED, "difficulty")
    values = args["words"]
    require(type(values) is list and 1 <= len(values) <= 30, "word_count")
    words = []
    seen = set()
    for value in values:
        label = utf8(value, 64, "word_label").strip(" ")
        require(bool(re.fullmatch(r"[A-Za-z][A-Za-z '\-]*[A-Za-z]|[A-Za-z]", label)), "word_label")
        letters = re.sub(r"[ '\-]", "", label).upper()
        require(3 <= len(letters) <= 18, "word_length")
        require(letters not in seen and letters[::-1] not in seen, "word_duplicate_or_reverse")
        seen.add(letters)
        words.append({"id": len(words), "label": label, "letters": letters})
    size = integer(args["size"], 8, 24, "requested_size") if "size" in args else 0
    require(not size or all(len(word["letters"]) <= size for word in words), "word_exceeds_size")
    seed_fields = [VERSION + "-seed", title, args["difficulty"], str(size), str(len(words))]
    for word in words:
        seed_fields.extend((word["label"], word["letters"]))
    seed = (integer(args["seed"], 0, 2_147_483_647, "requested_seed") if "seed" in args
            else struct.unpack(">I", digest(seed_fields)[:4])[0] & 0x7fffffff)
    return {"title": title, "difficulty": args["difficulty"], "requested_size": size,
            "seed": seed, "words": words}


def physical_paths(grid: list[str], letters: str) -> set[tuple[tuple[int, int], ...]]:
    """Enumerate all eight directions; deduplicate a path and its reversal."""
    size = len(grid)
    found = set()
    for row in range(size):
        for column in range(size):
            for dr, dc in DIRECTIONS.values():
                last_row = row + dr * (len(letters) - 1)
                last_column = column + dc * (len(letters) - 1)
                if not (0 <= last_row < size and 0 <= last_column < size):
                    continue
                cells = tuple((row + dr * offset, column + dc * offset) for offset in range(len(letters)))
                if "".join(grid[r][c] for r, c in cells) == letters:
                    found.add(min(cells, cells[::-1]))
    return found


def coordinate(value: object, size: int) -> tuple[int, int]:
    point = object_keys(value, {"row", "column"}, "coordinate_fields")
    return (integer(point["row"], 0, size - 1, "coordinate_bounds"),
            integer(point["column"], 0, size - 1, "coordinate_bounds"))


class SafeSheet(HTMLParser):
    ALLOWED_TAGS = {"html", "head", "meta", "title", "style", "body", "header", "main", "footer",
                    "section", "h1", "h2", "h3", "p", "div", "span", "ol", "ul", "li", "table",
                    "thead", "tbody", "tr", "th", "td", "strong", "b", "br", "small"}

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.csp = False
        self.in_style = False
        self.in_grid = False
        self.in_words = False
        self.in_heading = False
        self.in_cell = False
        self.in_word = False
        self.heading = ""
        self.cell = ""
        self.word = ""
        self.grid: list[str] = []
        self.row: list[str] = []
        self.words: list[str] = []
        self.highlight: set[tuple[int, int]] = set()
        self.has_coordinates = False

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        require(tag in self.ALLOWED_TAGS, "active_html_tag")
        for key, value in attrs:
            require(not key.startswith("on") and key not in {"src", "href", "srcset", "action", "formaction",
                                                             "xlink:href", "background", "srcdoc"}, "active_html_attribute")
            if key == "style" and value:
                self.check_css(value)
        attr = dict(attrs)
        if tag == "meta":
            require(attr.get("http-equiv", "").lower() in {"", "content-security-policy"}, "active_html_meta")
        if tag == "meta" and attr.get("http-equiv", "").lower() == "content-security-policy":
            require("default-src 'none'" in attr.get("content", ""), "html_csp")
            self.csp = True
        self.in_style = tag == "style" or self.in_style
        if tag == "h1":
            self.in_heading = True
        if tag == "table":
            require(not self.in_grid and attr.get("class") == "grid", "html_grid_table")
            self.in_grid = True
        if tag == "tr" and self.in_grid:
            self.row = []
        if tag == "td" and self.in_grid:
            self.in_cell = True
            self.cell = ""
            if "found" in attr.get("class", "").split():
                self.highlight.add((len(self.grid), len(self.row)))
        if tag == "ul" and attr.get("class") == "words":
            self.in_words = True
        if tag == "li" and self.in_words:
            self.in_word = True
            self.word = ""
        if tag == "ol" and attr.get("class") == "coordinates":
            self.has_coordinates = True

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.handle_starttag(tag, attrs)
        self.handle_endtag(tag)

    def handle_endtag(self, tag: str) -> None:
        require(tag in self.ALLOWED_TAGS, "active_html_tag")
        if tag == "style":
            self.in_style = False
        if tag == "h1":
            self.in_heading = False
        if tag == "td" and self.in_grid:
            require(bool(re.fullmatch("[A-Z]", self.cell)), "html_grid_cell")
            self.row.append(self.cell)
            self.in_cell = False
        if tag == "tr" and self.in_grid:
            self.grid.append("".join(self.row))
        if tag == "table":
            self.in_grid = False
        if tag == "li" and self.in_words:
            self.words.append(self.word)
            self.in_word = False
        if tag == "ul":
            self.in_words = False

    @staticmethod
    def check_css(value: str) -> None:
        require(not re.search(r"@import|url\s*\(|expression\s*\(|\\", value, re.I), "active_css")

    def handle_data(self, data: str) -> None:
        if self.in_style:
            self.check_css(data)
        if self.in_heading:
            self.heading += data
        if self.in_cell:
            self.cell += data
        if self.in_word:
            self.word += data


def validate_sheet(value: object, result: dict, answer_key: bool) -> None:
    html = utf8(value, HTML_LIMIT, "html_bytes")
    require(html.startswith("<!doctype html>"), "html_doctype")
    parser = SafeSheet()
    parser.feed(html)
    parser.close()
    require(parser.csp, "html_csp")
    require(parser.heading == result["title"] and parser.grid == result["grid"]
            and parser.words == [word["label"] for word in result["words"]], "html_source_binding")
    expected_highlight = ({(cell["row"], cell["column"]) for placement in result["placements"]
                           for cell in placement["cells"]} if answer_key else set())
    require(parser.highlight == expected_highlight and parser.has_coordinates == answer_key, "html_answer_separation")


def puzzle_id(result: dict) -> str:
    fields = [VERSION, result["title"], result["difficulty"], str(result["size"]),
              str(result["seed"]), str(len(result["words"]))]
    for word in result["words"]:
        fields.extend((word["label"], word["letters"]))
    fields.extend(result["grid"])
    for placement in result["placements"]:
        fields.extend((placement["direction"], str(placement["start"]["row"]), str(placement["start"]["column"]),
                       str(placement["end"]["row"]), str(placement["end"]["column"])))
    return digest(fields).hex()


def validate_fixture(value: object) -> None:
    fixture = object_keys(value, {"input", "response"}, "fixture_fields")
    require(len(json_bytes(fixture["input"])) <= REQUEST_LIMIT, "request_bytes")
    expected = canonical_call(fixture["input"])
    response = object_keys(fixture["response"], {"ok", "tool", "result"}, "response_fields")
    require(response["ok"] is True and response["tool"] == "make_word_search", "response_envelope")
    require(len(json_bytes(response)) <= RESPONSE_LIMIT, "response_bytes")
    result = object_keys(response["result"], RESULT_KEYS, "result_fields")
    require(type(result["schema_version"]) is int and result["schema_version"] == 1, "schema_version")
    require(result["generator_version"] == VERSION, "generator_version")
    require(result["title"] == expected["title"] and result["difficulty"] == expected["difficulty"], "input_title_or_difficulty")
    size = integer(result["size"], 8, 24, "result_size")
    require(not expected["requested_size"] or size == expected["requested_size"], "input_size")
    require(all(len(word["letters"]) <= size for word in expected["words"]), "grid_too_small")
    require(integer(result["seed"], 0, 2_147_483_647, "result_seed") == expected["seed"], "input_seed")
    require(result["directions"] == ALLOWED[result["difficulty"]], "directions")
    require(type(result["grid"]) is list and len(result["grid"]) == size, "grid_rows")
    grid = result["grid"]
    require(all(type(row) is str and bool(re.fullmatch(r"[A-Z]{" + str(size) + "}", row)) for row in grid), "grid_cells")
    require(type(result["words"]) is list and result["words"] == expected["words"], "input_words")
    for word in result["words"]:
        object_keys(word, {"id", "label", "letters"}, "word_fields")
        require(type(word["id"]) is int, "word_id")
    placements = result["placements"]
    require(type(placements) is list and len(placements) == len(expected["words"]), "placement_count")
    for index, placement in enumerate(placements):
        object_keys(placement, {"word_id", "direction", "start", "end", "cells"}, "placement_fields")
        require(type(placement["word_id"]) is int and placement["word_id"] == index, "placement_word_id")
        direction = placement["direction"]
        require(type(direction) is str and direction in result["directions"], "placement_direction")
        start = coordinate(placement["start"], size)
        end = coordinate(placement["end"], size)
        letters = expected["words"][index]["letters"]
        require(type(placement["cells"]) is list and len(placement["cells"]) == len(letters), "placement_cells")
        cells = tuple(coordinate(point, size) for point in placement["cells"])
        dr, dc = DIRECTIONS[direction]
        require(cells == tuple((start[0] + dr * i, start[1] + dc * i) for i in range(len(letters)))
                and cells[-1] == end, "placement_geometry")
        require("".join(grid[row][column] for row, column in cells) == letters, "placement_letters")
        matches = physical_paths(grid, letters)
        require(len(matches) == 1 and min(cells, cells[::-1]) in matches, "physical_occurrence_uniqueness")
    require(type(result["puzzle_id"]) is str and bool(re.fullmatch("[0-9a-f]{64}", result["puzzle_id"])), "puzzle_id_shape")
    require(result["puzzle_id"] == puzzle_id(result), "puzzle_id_binding")
    require(result["filename"] == "word-search.html" and result["media_type"] == "text/html; charset=utf-8", "artifact_metadata")
    for key, expected_flag in (("offline", True), ("network_requests", False), ("preview_requires_scripts", False),
                               ("puzzle_verified", True), ("content_verified", False)):
        require(result[key] is expected_flag, "safety_flag")
    validate_sheet(result["html"], result, False)
    validate_sheet(result["answer_key_html"], result, True)


def self_test(fixtures: list[dict]) -> int:
    # Guard the independent occurrence counter itself with known geometry.
    require(len(physical_paths(["LEVELZZZ"] + ["ZZZZZZZZ"] * 7, "LEVEL")) == 1, "self_palindrome")
    require(len(physical_paths(["AAAAZZZZ"] + ["ZZZZZZZZ"] * 7, "AAA")) == 2, "self_substring")
    require(len(physical_paths(["ZZZZZZZZ"] * 7 + ["ZZZTACZZ"], "CAT")) == 1, "self_reverse_direction")
    canonical = {"tool": "make_word_search", "arguments": {"title": "Numeric fixture", "words": ["CANONICAL"],
                  "difficulty": "easy", "size": 16, "seed": 10}}
    equivalent = copy.deepcopy(canonical)
    equivalent["arguments"].update(title="\u0085 Numeric fixture \u0085", size=Decimal("16.0"), seed=Decimal("1e1"))
    require(canonical_call(canonical) == canonical_call(equivalent), "self_canonical_numeric")
    rejected = 0

    def expect_rejection(value: object) -> None:
        nonlocal rejected
        try:
            validate_fixture(value)
        except InvalidFixture:
            rejected += 1
            return
        raise InvalidFixture("self_mutation_accepted")

    for fixture in fixtures:
        mutations = [
            ("result", "schema_version", True), ("result", "generator_version", "wordsearch-v2"),
            ("result", "title", "Changed synthetic title"), ("result", "seed", -1),
            ("result", "size", 7), ("result", "directions", ["W"]),
            ("result", "puzzle_id", "0" * 64), ("result", "network_requests", True),
            ("result", "puzzle_verified", False), ("result", "content_verified", True),
            ("result", "html", "<!doctype html><script>alert(1)</script>"),
            ("result", "answer_key_html", "<!doctype html><iframe src='https://example.invalid'></iframe>"),
        ]
        for target, key, replacement in mutations:
            changed = copy.deepcopy(fixture)
            changed["response"][target][key] = replacement
            expect_rejection(changed)
        for field in ("title", "words", "seed", "size"):
            changed = copy.deepcopy(fixture)
            changed["input"]["arguments"][field] = {
                "title": "Different synthetic request", "words": ["OTHER"],
                "seed": Decimal("1.5"), "size": True,
            }[field]
            expect_rejection(changed)
        changed = copy.deepcopy(fixture)
        changed["response"]["result"]["placements"][0]["cells"][0]["row"] = -1
        expect_rejection(changed)
        changed = copy.deepcopy(fixture)
        changed["response"]["result"]["words"][0]["id"] = True
        expect_rejection(changed)
        changed = copy.deepcopy(fixture)
        changed["response"]["result"]["unknown"] = True
        expect_rejection(changed)
        for field, insertion, boundary in (
            ("html", "<script>alert(1)</script>", "</body>"),
            ("html", "<meta http-equiv='refresh' content='0;url=https://example.invalid'>", "</head>"),
            ("answer_key_html", "<img src='https://example.invalid'>", "</body>"),
        ):
            changed = copy.deepcopy(fixture)
            original = changed["response"]["result"][field]
            require(boundary in original, "self_html_boundary")
            changed["response"]["result"][field] = original.replace(boundary, insertion + boundary, 1)
            expect_rejection(changed)
        changed = copy.deepcopy(fixture)
        sheet = changed["response"]["result"]["html"]
        require("<td>" in sheet, "self_html_cell")
        changed["response"]["result"]["html"] = sheet.replace("<td>", '<td class="found">', 1)
        expect_rejection(changed)
        changed = copy.deepcopy(fixture)
        changed["response"]["result"]["answer_key_html"] = changed["response"]["result"]["html"]
        expect_rejection(changed)
        # Defeat an id-only validator: copy a valid target onto a separate row,
        # then recompute its content hash. The physical check must still fail.
        changed = copy.deepcopy(fixture)
        result = changed["response"]["result"]
        word = result["words"][0]["letters"]
        occupied = {(point["row"], point["column"]) for p in result["placements"] for point in p["cells"]}
        for row in range(result["size"]):
            cells = {(row, column) for column in range(len(word))}
            if cells.isdisjoint(occupied):
                result["grid"][row] = word + result["grid"][row][len(word):]
                result["puzzle_id"] = puzzle_id(result)
                expect_rejection(changed)
                break
    return rejected


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="*", type=Path,
                        help="fixed synthetic fixture files or directories")
    parser.add_argument("--self-test", action="store_true", help="also reject adversarial mutations")
    args = parser.parse_args()
    paths = args.paths or [Path(__file__).resolve().parents[1] / "services/study-host/tests/fixtures/word-search"]
    files = sorted({child for path in paths for child in (path.glob("*.json") if path.is_dir() else [path])})
    require(bool(files), "fixtures_missing")
    fixtures = []
    for path in files:
        require(path.stat().st_size <= REQUEST_LIMIT + RESPONSE_LIMIT + 16_384, "fixture_bytes")
        fixture = load_json(path.read_bytes())
        validate_fixture(fixture)
        fixtures.append(fixture)
    mutations = self_test(fixtures) if args.self_test else 0
    print(f"Word-search fixtures passed: {len(fixtures)} independent source/grid/hash checks; {mutations} rejected mutations.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (InvalidFixture, OSError) as error:
        # No paths, words, titles, HTML, network data, or raw exception details.
        print("Word-search fixtures failed: " + (str(error) if isinstance(error, InvalidFixture) else "fixture_io"), file=sys.stderr)
        sys.exit(1)
