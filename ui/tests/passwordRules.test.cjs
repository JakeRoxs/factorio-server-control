const {test} = require("node:test");
const assert = require("node:assert/strict");
const {isValidPassword} = require("../App/components/passwordRules.cjs");

test("new-password UI validation matches the backend UTF-8 byte limits", () => {
    for (const value of ["a".repeat(10), "a".repeat(72), "é".repeat(36), "🔒".repeat(18)]) {
        assert.equal(isValidPassword(value), true);
    }
    for (const value of [null, undefined, 42, "", "too-short", "a".repeat(73), "é".repeat(37), "🔒".repeat(19)]) {
        assert.equal(isValidPassword(value), false);
    }
});
