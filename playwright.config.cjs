const {defineConfig, devices} = require("@playwright/test");

module.exports = defineConfig({
    testDir: "./ui/e2e",
    fullyParallel: true,
    forbidOnly: Boolean(process.env.CI),
    retries: 0,
    workers: 2,
    timeout: 30000,
    reporter: "list",
    use: {
        baseURL: "http://127.0.0.1:18089",
        trace: "retain-on-failure",
        screenshot: "only-on-failure"
    },
    projects: [
        {name: "chromium", use: {...devices["Desktop Chrome"], viewport: {width: 1920, height: 1080}}},
        {name: "firefox", use: {...devices["Desktop Firefox"]}},
        {name: "webkit", use: {...devices["Desktop Safari"]}},
        {name: "mobile-chromium", use: {...devices["Pixel 7"]}},
        {name: "mobile-webkit", use: {...devices["iPhone 13"]}}
    ],
    webServer: {
        command: "node ui/e2e/fixture-server.cjs",
        url: "http://127.0.0.1:18089",
        reuseExistingServer: false,
        timeout: 10000
    }
});
