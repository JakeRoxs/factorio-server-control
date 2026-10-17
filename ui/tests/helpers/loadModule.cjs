const fs = require("node:fs");
const path = require("node:path");
const {transformSync} = require("@babel/core");

// Exercise the real browser modules with explicit boundaries, without a network
// connection or a second copy of their implementation in the tests.
module.exports = function loadModule(filename, imports = {}, globals = {}) {
    const code = transformSync(fs.readFileSync(path.join(__dirname, "../..", filename), "utf8"), {
        presets: [["@babel/preset-env", {targets: {node: "current"}, modules: "commonjs"}], "@babel/preset-react"]
    }).code;
    const module = {exports: {}};
    const requireDependency = name => {
        if (!Object.hasOwn(imports, name)) throw new Error(`Unexpected test dependency: ${name}`);
        return imports[name];
    };
    new Function("require", "module", "exports", ...Object.keys(globals), code)(
        requireDependency, module, module.exports, ...Object.values(globals)
    );
    return module.exports;
};
