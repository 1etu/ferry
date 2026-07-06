import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { extname, join, relative } from "node:path";

const checkedExtensions = new Set([".go", ".ts", ".js", ".mjs", ".svelte", ".css", ".html"]);
const skippedDirectories = new Set(["node_modules", "dist", "bin", ".svelte-kit", "docs", ".claude", "coverage", "test-results", "playwright-report"]);
const allowedDirectives = /^\s*\/\/(go:|nolint)/;

const linePatterns = [
  { pattern: /^\s*\/\/(?!go:|nolint)/, kind: "line comment" },
  { pattern: /^\s*\/\*/, kind: "block comment" },
  { pattern: new RegExp(["<", "!--"].join("")), kind: "html comment" },
  { pattern: /[\w;,)\]}'"`]\s+\/\/\s/, kind: "trailing comment" },
  { pattern: /[\w;,)\]}'"`]\s+\/\*/, kind: "trailing block comment" },
];

function findViolations(path) {
  const lines = readFileSync(path, "utf8").split(/\r?\n/);
  const violations = [];
  lines.forEach((line, index) => {
    if (allowedDirectives.test(line)) return;
    const match = linePatterns.find(({ pattern }) => pattern.test(line));
    if (match) violations.push(`${relative(process.cwd(), path)}:${index + 1}: ${match.kind}: ${line.trim()}`);
  });
  return violations;
}

function walk(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    if (statSync(path).isDirectory()) return skippedDirectories.has(name) ? [] : walk(path);
    return checkedExtensions.has(extname(name)) ? [path] : [];
  });
}

function expand(path) {
  if (!existsSync(path)) return [];
  return statSync(path).isDirectory() ? walk(path) : [path];
}

const targets = process.argv.length > 2 ? process.argv.slice(2).flatMap(expand) : walk(process.cwd());
const violations = targets
  .filter((path) => checkedExtensions.has(extname(path)))
  .flatMap(findViolations);

if (violations.length > 0) {
  console.error(`Comments are not allowed in this project:\n${violations.join("\n")}`);
  process.exit(1);
}
