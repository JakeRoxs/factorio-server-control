const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {act, create} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("locked save uploads keep controls visible but disabled and cannot submit", async t => {
    let uploads = 0;
    const Form = loadModule("App/views/Saves/components/UploadSaveForm.jsx", {
        react: React,
        "react-hook-form": {useForm: () => ({register: () => ({}), handleSubmit: fn => fn, reset() {}, formState: {errors: {}}})},
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {faCloudArrowUp: {}},
        "../../../../api/resources/saves": {upload: async () => { uploads++; }},
        "../../../components/Button": props => React.createElement("button", {disabled: props.isDisabled || props.isLoading}, props.children),
        "../../../components/Error": () => null,
        "../../../components/Label": () => null
    }, {window: {flash() {}}}).default;
    let root;
    act(() => { root = create(React.createElement(Form, {disabled: true, onSuccess: async () => {}})); });
    t.after(() => act(() => root.unmount()));
    assert.equal(root.root.findByType("input").props.disabled, true);
    assert.equal(root.root.findByType("button").props.disabled, true);
    await act(async () => root.root.findByType("form").props.onSubmit({savefile: [{}]}));
    assert.equal(uploads, 0);
    act(() => root.update(React.createElement(Form, {disabled: false, onSuccess: async () => {}})));
    assert.equal(root.root.findByType("input").props.disabled, false);
    await act(async () => root.root.findByType("form").props.onSubmit({savefile: [{}]}));
    assert.equal(uploads, 1);
});
