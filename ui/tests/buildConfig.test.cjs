const {test} = require("node:test");
const assert = require("node:assert/strict");
const configure = require("../../webpack.config.js");
const CssMinimizerPlugin = require("css-minimizer-webpack-plugin");
const TerserPlugin = require("terser-webpack-plugin");

test("production minifies both stylesheet and JavaScript assets", () => {
    const config = configure({}, {mode: "production"});
    assert.equal(config.optimization.minimize, true);
    assert.ok(config.optimization.minimizer.some(plugin => plugin instanceof CssMinimizerPlugin));
    assert.ok(config.optimization.minimizer.some(plugin => plugin instanceof TerserPlugin));
    assert.equal(config.devtool, false);
});

test("development retains source maps without minification", () => {
    const config = configure({}, {mode: "development"});
    assert.equal(config.optimization.minimize, false);
    assert.equal(config.devtool, "source-map");
});

test("lazy pages use root-relative content-hashed assets across navigation and updates", () => {
    const config = configure({}, {mode: "production"});
    assert.equal(config.output.publicPath, "/");
    assert.match(config.output.chunkFilename, /^chunks\/.*\[contenthash/);
    assert.equal(config.output.clean, true);
});
