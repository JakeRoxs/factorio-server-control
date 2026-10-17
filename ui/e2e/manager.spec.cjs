const {test, expect} = require("@playwright/test");

test.beforeEach(async ({page}) => {
    page.testFailures = [];
    page.on("pageerror", error => {
        if (!page.expectChunkFailure || !/chunk/i.test(error.message)) page.testFailures.push(error.message);
    });
    page.on("response", response => {
        if (response.status() === 501) page.testFailures.push(`Missing fixture: ${response.url()}`);
    });
    // No real socket or console commands, even when a scenario says running.
    await page.routeWebSocket("**/ws", () => {});
});

test.afterEach(async ({page}) => {
    expect(page.testFailures).toEqual([]);
});

async function navigate(page, label) {
    const nav = page.getByRole("navigation", {name: "Primary navigation"});
    if (!(await nav.isVisible())) await page.getByRole("button", {name: "Open navigation", exact: true}).click();
    await nav.getByRole("link", {name: label, exact: true}).click();
}

const routes = [
    ["/", "Overview"], ["/saves", "Saves & checkpoints"], ["/mods", "Mods & mod packs"],
    ["/server-settings", "Server settings"], ["/game-settings", "Game settings"],
    ["/console", "Console"], ["/logs", "Factorio logs"], ["/releases", "Version & mode"],
    ["/profiles", "Profiles"], ["/user-management", "Users & access"]
];

test("every authenticated page opens directly with its own lazy assets", async ({page}) => {
    for (const [url, title] of routes) {
        const response = await page.goto(url);
        expect(response.headers()["cache-control"]).toContain("must-revalidate");
        await expect(page.getByRole("heading", {level: 1, name: title, exact: true})).toBeVisible();
        await expect(page.getByRole("status", {name: "Loading page…"})).toHaveCount(0);
        await expect(page.getByText("This page could not be loaded.", {exact: false})).toHaveCount(0);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    }
});

test("navigation loads only the requested page and preserves the dark palette", async ({page}) => {
    const chunks = [];
    page.on("request", request => { if (request.url().includes("/chunks/")) chunks.push(request.url()); });
    await page.goto("/");
    await expect(page.getByRole("heading", {level: 1, name: "Overview"})).toBeVisible();
    expect(chunks.some(url => /\/overview\./.test(url))).toBe(true);
    expect(chunks.some(url => /\/mods\./.test(url))).toBe(false);
    await navigate(page, "Mods");
    await expect(page.getByRole("heading", {level: 1, name: "Mods & mod packs"})).toBeVisible();
    expect(chunks.some(url => /\/mods\./.test(url))).toBe(true);
    await expect(page.locator(".text-orange").first()).toHaveCSS("color", "rgb(244, 166, 42)");
    await page.goBack();
    await expect(page.getByRole("heading", {level: 1, name: "Overview"})).toBeVisible();
});

test("a missing page chunk retains navigation and offers an explicit reload", async ({page}) => {
    page.expectChunkFailure = true;
    await page.route("**/chunks/logs.*.js", route => route.fulfill({status: 404, body: ""}));
    await page.goto("/logs");
    await expect(page.getByRole("button", {name: "Reload page"})).toBeVisible();
    await navigate(page, "Saves & Checkpoints");
    await expect(page.getByRole("heading", {level: 1, name: "Saves & checkpoints"})).toBeVisible();
    await page.unroute("**/chunks/logs.*.js");
    await navigate(page, "Logs");
    await page.getByRole("button", {name: "Reload page"}).click();
    await expect(page.getByRole("heading", {level: 1, name: "Factorio logs"})).toBeVisible();
    await expect(page.getByText("Synthetic test log", {exact: false})).toBeVisible();
});

test("running and unknown process states prevent destructive edits", async ({page}) => {
    for (const scenario of ["running", "unknown"]) {
        await page.setExtraHTTPHeaders({"X-FSM-Test-Scenario": scenario});
        await page.goto("/saves");
        await expect(page.getByRole("button", {name: "Delete demo.zip"})).toBeDisabled();
        await expect(page.locator('input[type="file"]')).toBeDisabled();
        await expect(page.getByRole("link", {name: "Download demo.zip"})).toBeVisible();
        await navigate(page, "Server settings");
        await expect(page.getByRole("textbox", {name: "Name", exact: true})).toBeDisabled();
        await navigate(page, "Version & mode");
        await expect(page.getByRole("radio", {name: /Experimental/})).toBeDisabled();
        await expect(page.getByRole("radio", {name: /Experimental/})).toBeChecked();
    }
});

test("viewer pages do not expose mutations or the admin console", async ({page}) => {
    await page.setExtraHTTPHeaders({"X-FSM-Test-Scenario": "viewer"});
    await page.goto("/saves");
    await expect(page.getByRole("link", {name: "Download demo.zip"})).toBeVisible();
    await expect(page.getByRole("button", {name: "Delete demo.zip"})).toHaveCount(0);
    await page.goto("/console");
    await expect(page.getByRole("heading", {level: 1, name: "Overview"})).toBeVisible();
    await page.goto("/user-management");
    await expect(page.getByRole("heading", {level: 1, name: "Account & access"})).toBeVisible();
    await expect(page.getByRole("heading", {name: "Create user"})).toHaveCount(0);
});

test("failed save deletion keeps the save and its confirmation recoverable", async ({page}) => {
    await page.goto("/saves");
    const remove = page.getByRole("button", {name: "Delete demo.zip"});
    await remove.click();
    const dialog = page.getByRole("dialog", {name: "Delete save?"});
    await dialog.getByRole("button", {name: "Confirm", exact: true}).click();
    await expect(page.getByText("Synthetic operation failed; existing data was kept.").first()).toBeVisible();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", {name: "Cancel"}).click();
    await expect(dialog).toHaveCount(0);
    await expect(remove).toBeFocused();
    await expect(page.getByRole("link", {name: "Download demo.zip"})).toBeVisible();
});

test("failed settings save preserves the draft", async ({page}) => {
    await page.goto("/server-settings");
    const name = page.getByRole("textbox", {name: "Name", exact: true});
    await expect(name).toBeEnabled();
    await name.fill("Unsaved browser test");
    await page.getByRole("button", {name: "Save changes", exact: true}).click();
    await expect(page.getByText("Synthetic operation failed; existing data was kept.").first()).toBeVisible();
    await expect(name).toHaveValue("Unsaved browser test");
    await expect(page.getByRole("button", {name: "Discard changes", exact: true})).toBeEnabled();
});

test("a failed log request can be retried without reloading the application", async ({page}) => {
    await page.route("**/api/log/tail**", route => route.fulfill({status: 503, contentType: "text/plain", body: "Unavailable"}), {times: 1});
    await page.goto("/logs");
    await expect(page.getByRole("alert")).toContainText("Factorio logs could not be loaded");
    await page.getByRole("button", {name: "Refresh", exact: true}).click();
    await expect(page.getByText("Synthetic test log", {exact: false})).toBeVisible();
});

test("profile picker works from the keyboard and links to new profile setup", async ({page}) => {
    await page.goto("/");
    const openNavigation = page.getByRole("button", {name: "Open navigation", exact: true});
    if (await openNavigation.isVisible()) await openNavigation.click();
    const picker = page.getByRole("button", {name: /Active profile Demo factory/});
    await picker.focus();
    await page.keyboard.press("Enter");
    await expect(picker).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");
    await expect(picker).toHaveAttribute("aria-expanded", "false");
    if (await openNavigation.isVisible()) await openNavigation.click();
    await picker.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("link", {name: "New profile", exact: true}).click();
    await expect(page).toHaveURL(/\/profiles\?new=fresh$/);
    await expect(page.getByRole("heading", {level: 1, name: "Profiles"})).toBeVisible();
});

test("anonymous deep links lead to login without loading protected page chunks", async ({page}) => {
    await page.setExtraHTTPHeaders({"X-FSM-Test-Scenario": "anonymous"});
    const chunks = [];
    page.on("request", request => { if (request.url().includes("/chunks/")) chunks.push(request.url()); });
    await page.goto("/mods");
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('input[type="password"]')).toBeVisible();
    expect(chunks).toEqual([]);
});
