import { safeWordSearchHTML } from "./word-search-html.js";
import { strictStudyJSON } from "./contract.js";
export { strictStudyJSON };
export const WORD_SEARCH_REQUEST_BYTES = 16_384;
export const WORD_SEARCH_RESPONSE_BYTES = 262_144;
export const WORD_SEARCH_HTML_BYTES = 98_304;
const encoder = new TextEncoder();
const directions = {
  easy: ["E", "S"], medium: ["E", "S", "SE", "NE"], hard: ["E", "S", "SE", "NE", "W", "N", "NW", "SW"],
} as const;
const vector: Record<string, readonly [number, number]> = { E: [0, 1], S: [1, 0], SE: [1, 1], NE: [-1, 1], W: [0, -1], N: [-1, 0], NW: [-1, -1], SW: [1, -1] };
type Difficulty = keyof typeof directions;
interface Point { row: number; column: number }
interface Word { id: number; label: string; letters: string }
interface Placement { word_id: number; direction: string; start: Point; end: Point; cells: Point[] }
export interface WordSearchCall {
  tool: "make_word_search";
  arguments: { title: string; words: string[]; difficulty: Difficulty; size?: number; seed?: number };
}
function object(raw: unknown, required: string[], optional: string[] = []): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw Error("invalid_request");
  const keys = Object.keys(raw);
  if (required.some(key => !Object.hasOwn(raw, key)) || keys.some(key => !required.includes(key) && !optional.includes(key))) throw Error("invalid_request");
  return raw as Record<string, unknown>;
}
function integer(raw: unknown, minimum: number, maximum: number): number {
  if (typeof raw !== "number" || !Number.isSafeInteger(raw) || raw < minimum || raw > maximum) throw Error("invalid_request");
  return raw;
}
// Go's Unicode TrimSpace table, intentionally distinct from JavaScript trim's
// BOM handling. Word labels use ASCII outer spaces only.
function title(raw: unknown): string {
  if (typeof raw !== "string" || raw.includes("\0") || encoder.encode(raw).length > 120) throw Error("invalid_request");
  const value = raw.replace(/^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/gu, "");
  if (!value) throw Error("invalid_request");
  return value;
}
function word(raw: unknown, id: number): Word {
  if (typeof raw !== "string" || encoder.encode(raw).length > 64) throw Error("invalid_request");
  const label = raw.replace(/^ +| +$/g, "");
  if (!/^[A-Za-z]+(?:[ '\-]+[A-Za-z]+)*$/.test(label)) throw Error("invalid_request");
  const letters = label.replace(/[ '\-]/g, "").toUpperCase();
  if (letters.length < 3 || letters.length > 18) throw Error("invalid_request");
  return { id, label, letters };
}
export function wordSearchCall(raw: unknown): WordSearchCall {
  const root = object(raw, ["tool", "arguments"]);
  if (root.tool !== "make_word_search") throw Error("invalid_request");
  const args = object(root.arguments, ["title", "words", "difficulty"], ["size", "seed"]);
  const cleanTitle = title(args.title);
  if (typeof args.difficulty !== "string" || !Object.hasOwn(directions, args.difficulty)) throw Error("invalid_request");
  if (!Array.isArray(args.words) || args.words.length < 1 || args.words.length > 30) throw Error("invalid_request");
  const words = args.words.map(word), seen = new Set<string>();
  for (const value of words) {
    if (seen.has(value.letters) || seen.has([...value.letters].reverse().join(""))) throw Error("invalid_request");
    seen.add(value.letters);
  }
  const size = Object.hasOwn(args, "size") ? integer(args.size, 8, 24) : undefined;
  const seed = Object.hasOwn(args, "seed") ? integer(args.seed, 0, 2_147_483_647) : undefined;
  if (size !== undefined && words.some(value => value.letters.length > size)) throw Error("invalid_request");
  const call: WordSearchCall = { tool: "make_word_search", arguments: { title: cleanTitle, words: words.map(value => value.label), difficulty: args.difficulty as Difficulty, ...(size === undefined ? {} : { size }), ...(seed === undefined ? {} : { seed }) } };
  if (encoder.encode(JSON.stringify(call)).length > WORD_SEARCH_REQUEST_BYTES) throw Error("payload_too_large");
  return call;
}
async function hash(fields: string[]): Promise<Uint8Array> {
  const bytes = fields.map(value => encoder.encode(value));
  const input = new Uint8Array(bytes.reduce((sum, value) => sum + 4 + value.byteLength, 0));
  const view = new DataView(input.buffer); let position = 0;
  for (const value of bytes) { view.setUint32(position, value.byteLength, false); position += 4; input.set(value, position); position += value.byteLength; }
  return new Uint8Array(await crypto.subtle.digest("SHA-256", input));
}
export async function wordSearchSeed(call: WordSearchCall): Promise<number> {
  if (call.arguments.seed !== undefined) return call.arguments.seed;
  const args = call.arguments, words = args.words.map(word);
  const digest = await hash(["wordsearch-v1-seed", args.title, args.difficulty, String(args.size ?? 0), String(words.length), ...words.flatMap(value => [value.label, value.letters])]);
  return new DataView(digest.buffer).getUint32(0, false) & 0x7fffffff;
}
function point(raw: unknown, size: number): Point {
  const value = object(raw, ["row", "column"]);
  return { row: integer(value.row, 0, size - 1), column: integer(value.column, 0, size - 1) };
}
function samePoint(left: Point, right: Point): boolean { return left.row === right.row && left.column === right.column; }
function physical(cells: Point[]): string {
  const forward = cells.map(value => `${value.row}:${value.column}`).join(",");
  const backward = [...cells].reverse().map(value => `${value.row}:${value.column}`).join(",");
  return forward < backward ? forward : backward;
}
function occurrences(grid: string[], letters: string): Set<string> {
  const result = new Set<string>(), size = grid.length;
  for (let row = 0; row < size; row++) for (let column = 0; column < size; column++) for (const [dr, dc] of Object.values(vector)) {
    const endRow = row + dr * (letters.length - 1), endColumn = column + dc * (letters.length - 1);
    if (endRow < 0 || endColumn < 0 || endRow >= size || endColumn >= size) continue;
    const cells: Point[] = []; let matches = true;
    for (let index = 0; index < letters.length; index++) {
      const cell = { row: row + dr * index, column: column + dc * index };
      if (grid[cell.row][cell.column] !== letters[index]) { matches = false; break; }
      cells.push(cell);
    }
    if (matches) result.add(physical(cells));
  }
  return result;
}
export async function wordSearchResult(raw: unknown, call: WordSearchCall): Promise<unknown> {
  const root = object(raw, ["ok", "tool", "result"]);
  if (root.ok !== true || root.tool !== "make_word_search") throw Error("invalid_response");
  const r = object(root.result, ["schema_version", "generator_version", "puzzle_id", "title", "difficulty", "size", "seed", "directions", "grid", "words", "placements", "filename", "media_type", "html", "answer_key_html", "offline", "network_requests", "preview_requires_scripts", "puzzle_verified", "content_verified"]);
  const size = integer(r.size, 8, 24), seed = integer(r.seed, 0, 2_147_483_647), args = call.arguments;
  if (r.schema_version !== 1 || r.generator_version !== "wordsearch-v1" || r.title !== args.title || r.difficulty !== args.difficulty || (args.size !== undefined && size !== args.size) || seed !== await wordSearchSeed(call)) throw Error("invalid_response");
  if (typeof r.puzzle_id !== "string" || !/^[a-f0-9]{64}$/.test(r.puzzle_id) || !Array.isArray(r.directions) || r.directions.join(",") !== directions[args.difficulty].join(",") || r.directions.length !== directions[args.difficulty].length) throw Error("invalid_response");
  if (r.filename !== "word-search.html" || r.media_type !== "text/html; charset=utf-8" || r.offline !== true || r.network_requests !== false || r.preview_requires_scripts !== false || r.puzzle_verified !== true || r.content_verified !== false) throw Error("invalid_response");
  for (const html of [r.html, r.answer_key_html]) if (typeof html !== "string" || !safeWordSearchHTML(html) || encoder.encode(html).length > WORD_SEARCH_HTML_BYTES) throw Error("invalid_response");
  // Printable questions must not contain the key's actual highlight/coordinate
  // markup. Both templates include the same inert CSS definitions in their head.
  if ((r.html as string).includes('<td class="found">') || (r.html as string).includes('<ol class="coordinates">') || !(r.answer_key_html as string).includes('<td class="found">') || !(r.answer_key_html as string).includes('<ol class="coordinates">')) throw Error("invalid_response");
  if (!Array.isArray(r.grid) || r.grid.length !== size || r.grid.some(row => typeof row !== "string" || !new RegExp(`^[A-Z]{${size}}$`).test(row))) throw Error("invalid_response");
  const grid = r.grid as string[], expected = args.words.map(word);
  if (!Array.isArray(r.words) || r.words.length !== expected.length || !Array.isArray(r.placements) || r.placements.length !== expected.length) throw Error("invalid_response");
  const placements: Placement[] = [];
  for (let id = 0; id < expected.length; id++) {
    const w = object(r.words[id], ["id", "label", "letters"]), target = expected[id];
    if (w.id !== id || w.label !== target.label || w.letters !== target.letters || target.letters.length > size) throw Error("invalid_response");
    const p = object(r.placements[id], ["word_id", "direction", "start", "end", "cells"]);
    if (p.word_id !== id || typeof p.direction !== "string" || !directions[args.difficulty].some(value => value === p.direction) || !Array.isArray(p.cells) || p.cells.length !== target.letters.length) throw Error("invalid_response");
    const start = point(p.start, size), end = point(p.end, size), cells = p.cells.map(value => point(value, size));
    const [dr, dc] = vector[p.direction];
    if (!samePoint(start, cells[0]) || !samePoint(end, cells[cells.length - 1])) throw Error("invalid_response");
    for (let index = 0; index < cells.length; index++) {
      const cell = cells[index];
      if (cell.row !== start.row + dr * index || cell.column !== start.column + dc * index || grid[cell.row][cell.column] !== target.letters[index]) throw Error("invalid_response");
    }
    const found = occurrences(grid, target.letters);
    if (found.size !== 1 || !found.has(physical(cells))) throw Error("invalid_response");
    placements.push({ word_id: id, direction: p.direction, start, end, cells });
  }
  const digest = await hash(["wordsearch-v1", args.title, args.difficulty, String(size), String(seed), String(expected.length), ...expected.flatMap(value => [value.label, value.letters]), ...grid, ...placements.flatMap(value => [value.direction, String(value.start.row), String(value.start.column), String(value.end.row), String(value.end.column)])]);
  const puzzleID = [...digest].map(value => value.toString(16).padStart(2, "0")).join("");
  if (puzzleID !== r.puzzle_id || encoder.encode(JSON.stringify(root)).length > WORD_SEARCH_RESPONSE_BYTES) throw Error("invalid_response");
  return raw;
}
