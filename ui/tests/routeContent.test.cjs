const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {act, create} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("pending and failed lazy pages keep the shell and never reload automatically", async t => {
    t.mock.method(console, "error", () => {}); // React reports the deliberately rejected chunk.
    let reloads = 0, rejectChunk;
    const RouteContent = loadModule("App/components/RouteContent.jsx", {
        react: React,
        "./Alert": ({children}) => React.createElement("div", {role: "alert"}, children),
        "./Button": ({children, onClick}) => React.createElement("button", {onClick}, children)
    }, {window: {location: {reload: () => reloads++}}}).default;
    const Page = React.lazy(() => new Promise((resolve, reject) => { rejectChunk = reject; }));
    const shell = key => React.createElement(React.Fragment, null,
        React.createElement("nav", null, "Navigation"),
        React.createElement(RouteContent, {key}, key === "broken" ? React.createElement(Page) : "Working page")
    );
    let root;
    await act(async () => { root = create(shell("broken")); });
    t.after(() => act(() => root.unmount()));
    assert.equal(root.root.findByProps({role: "status"}).children[0], "Loading page…");
    await act(async () => rejectChunk(new Error("Loading chunk failed")));
    assert.equal(root.root.findByType("nav").children[0], "Navigation");
    assert.equal(root.root.findAllByProps({role: "alert"}).length, 1);
    assert.equal(reloads, 0);
    act(() => root.root.findByType("button").props.onClick());
    assert.equal(reloads, 1);
    act(() => root.update(shell("healthy")));
    assert.equal(root.root.findAllByProps({role: "alert"}).length, 0);
    assert.ok(JSON.stringify(root.toJSON()).includes("Working page"));
});
