const {test} = require("node:test");
const assert = require("node:assert");
const fs = require("fs");

test("Saves.jsx guards against stale save list responses on server switch", async t => {
    const source = fs.readFileSync("ui/App/views/Saves/Saves.jsx", "utf8");
    assert.ok(source.includes("const requestedServer = serverID"), "must capture requested server before async call");
    assert.ok(source.includes("currentServerRef"), "must use ref to track current server");
    assert.ok(source.includes("requestedServer === currentServerRef.current"), "must guard state update with server comparison");
});

test("Checkpoints.jsx guards against stale checkpoint responses on server switch", async t => {
    const source = fs.readFileSync("ui/App/views/Saves/components/Checkpoints.jsx", "utf8");
    assert.ok(source.includes("const requestedServer = serverID"), "must capture requested server before async call");
    assert.ok(source.includes("currentServerRef"), "must use ref to track current server");
    assert.ok(source.includes("requestedServer === currentServerRef.current"), "must guard state update with server comparison");
});

test("Checkpoints.jsx guards all mutation completions against stale server", async t => {
    const source = fs.readFileSync("ui/App/views/Saves/components/Checkpoints.jsx", "utf8");
    // Count the number of guard checks - should be at least 5 (load, saveSettings, create, restore, remove)
    const guardCount = (source.match(/requestedServer === currentServerRef\.current/g) || []).length;
    assert.ok(guardCount >= 5, "must have guard checks for load and all mutation operations (found " + guardCount + ")");
});
