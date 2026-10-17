const test = require("node:test");
const assert = require("node:assert/strict");
const {createRequire} = require("node:module");

// Exercise the URI parser actually used by the build tools' AJV dependency.
const uri = createRequire(require.resolve("ajv"))("fast-uri");

test("build dependency normalizes percent-encoded host case consistently", () => {
    // Regression for GHSA-hrr3-gc8f-f4qj: scheme-relative hosts must agree.
    for (const reference of ["//a.com", "//A.com", "//%41.com", "//%61.com"]) {
        assert.equal(uri.parse(reference).host, "a.com", reference);
        assert.equal(uri.normalize(reference), "//a.com", reference);
        assert.equal(uri.equal(reference, "//a.com"), true, reference);
    }
});

test("all selector-parser build dependencies include the CPU-exhaustion fix", () => {
    const lock = require("../../package-lock.json");
    const semver = require("semver");
    const parsers = Object.entries(lock.packages)
        .filter(([path]) => path.endsWith("node_modules/postcss-selector-parser"));
    assert.ok(parsers.length > 0);
    for (const [path, metadata] of parsers) {
        assert.ok(semver.gte(metadata.version, "7.1.6"), `${path}: ${metadata.version}`);
    }
});

test("the build dependency tree no longer includes unpatched braces", () => {
    const lock = require("../../package-lock.json");
    assert.deepEqual(Object.keys(lock.packages).filter(path => path.endsWith("node_modules/braces")), []);
});

test("Tailwind builds the existing palette and responsive utilities from the real stylesheet", async () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const postcss = require("postcss");
    const from = path.resolve(__dirname, "../index.css");
    const result = await postcss([require("@tailwindcss/postcss")]).process(fs.readFileSync(from, "utf8"), {from});
    assert.match(result.css, /\.text-orange\s*\{\s*color: #F4A62A/i);
    assert.match(result.css, /\.sm\\:flex-row/);
    assert.match(result.css, /\.border-white\\\/5/);
    assert.match(result.css, /\.bg-orange\\\/10/);
    assert.match(result.css, /\.shrink-0/);
    assert.match(result.css, /\.ui-sidebar/);
    assert.doesNotMatch(result.css, /@import "tailwindcss"/);
});
