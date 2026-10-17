const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {act, create} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

const state = id => ({active_profile_id: id, profiles: [{id, name: id}]});
const deferred = () => {
    let resolve, reject;
    const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
    return {promise, resolve, reject};
};

async function mount(t) {
    const requests = [];
    const resource = {list() { const request = deferred(); requests.push(request); return request.promise; }};
    const {ProfileProvider, useProfiles} = loadModule("App/context/ProfileContext.jsx", {
        react: React, "../../api/resources/profiles": resource
    });
    let current, root;
    function Probe() { current = useProfiles(); return null; }
    await act(async () => { root = create(React.createElement(ProfileProvider, null, React.createElement(Probe))); });
    t.after(() => act(() => root.unmount()));
    return {requests, get current() { return current; }};
}

test("a late profile refresh cannot revert a completed profile mutation", async t => {
    const view = await mount(t);
    act(() => view.current.applyProfileState(state("new-profile")));
    await act(async () => view.requests[0].resolve(state("old-profile")));
    assert.equal(view.current.activeProfile.id, "new-profile");
    assert.equal(view.current.isLoading, false);
    assert.equal(view.current.error, "");
});

test("only the newest profile request can update data, error and loading state", async t => {
    const view = await mount(t);
    let latest;
    act(() => { latest = view.current.refreshProfiles(); });
    await act(async () => view.requests[0].reject(new Error("obsolete failure")));
    assert.equal(view.current.isLoading, true);
    assert.equal(view.current.error, "");
    await act(async () => { view.requests[1].resolve(state("current")); await latest; });
    assert.equal(view.current.activeProfile.id, "current");
    assert.equal(view.current.isLoading, false);
});

test("invalid active profile responses preserve previously validated state", async t => {
    const view = await mount(t);
    await act(async () => view.requests[0].resolve(state("original")));
    for (const invalid of [
        null, {profiles: [], active_profile_id: "missing"},
        {profiles: [null], active_profile_id: "missing"},
        {profiles: [{id: "duplicate"}, {id: "duplicate"}], active_profile_id: "duplicate"}
    ]) {
        assert.throws(() => act(() => view.current.applyProfileState(invalid)), /invalid response/);
        assert.equal(view.current.activeProfile.id, "original");
    }
});

test("a current failed refresh remains retryable and does not discard profile data", async t => {
    const view = await mount(t);
    await act(async () => view.requests[0].resolve(state("original")));
    let failed;
    act(() => { failed = view.current.refreshProfiles(); });
    await act(async () => {
        const rejection = assert.rejects(failed, /try again/);
        view.requests[1].reject(new Error("try again"));
        await rejection;
    });
    assert.equal(view.current.error, "try again");
    assert.equal(view.current.activeProfile.id, "original");
    let retry;
    act(() => { retry = view.current.refreshProfiles(); });
    await act(async () => { view.requests[2].resolve(state("updated")); await retry; });
    assert.equal(view.current.error, "");
    assert.equal(view.current.activeProfile.id, "updated");
});
