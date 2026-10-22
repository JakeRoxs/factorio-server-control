const {test, expect} = require("@playwright/test");

test.beforeEach(async ({page}) => {
    page.testFailures = [];
    page.on("pageerror", error => {
        if (!page.expectChunkFailure || !/chunk/i.test(error.message)) page.testFailures.push(error.message);
    });
    page.on("response", response => {
        if (response.status() === 501) page.testFailures.push(`Missing fixture: ${response.url()}`);
    });
    await page.route(/^https?:\/\//, route => {
        if (new URL(route.request().url()).origin === "http://127.0.0.1:18089") return route.continue();
        page.testFailures.push(`Unexpected external asset: ${route.request().url()}`);
        return route.abort();
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

async function disableNativePointerFocus(page) {
    // Safari does not focus buttons on pointer activation. Reproduce that
    // behavior on every engine so focus restoration cannot pass by accident.
    await page.addInitScript(() => document.addEventListener("mousedown", event => {
        if (event.target.closest("button")) event.preventDefault();
    }));
}

const routes = [
    ["/", "Overview"], ["/saves", "Saves & checkpoints"], ["/mods", "Mods & mod packs"],
    ["/server-settings", "Server settings"], ["/game-settings", "Game settings"],
    ["/console", "Console"], ["/logs", "Factorio logs"], ["/releases", "Version & mode"],
    ["/profiles", "Profiles"], ["/servers", "Servers"], ["/user-management", "Users & access"]
];

test("existing running instance is visible as one managed server", async ({page}) => {
    await page.setExtraHTTPHeaders({"x-fsm-test-scenario": "running"});
    await page.goto("/servers");
    await expect(page.getByRole("heading", {level: 3, name: "Existing server", exact: true})).toHaveCount(1);
    const card = page.locator("section").filter({has: page.getByRole("heading", {level: 3, name: "Existing server", exact: true})});
    await expect(card.getByText("Running", {exact: true})).toBeVisible();
    await expect(card.getByRole("button", {name: "Stop", exact: true})).toBeVisible();
    await expect(card.getByRole("button", {name: "Start", exact: true})).toHaveCount(0);
});

test("running-server allocator preference survives navigation and reload without stopping the game", async ({page}) => {
    await page.setExtraHTTPHeaders({"x-fsm-test-scenario": "running"});
    let enabled = false;
    const mutations = [];
    const server = () => ({id: "1", name: "Existing server", port: 34197, bindip: "0.0.0.0", running: true, use_mimalloc: enabled});
    await page.route("**/api/servers", route => route.fulfill({json: [server()]}));
    await page.route("**/api/servers/1", async route => {
        expect(route.request().method()).toBe("PUT");
        const body = route.request().postDataJSON();
        expect(Object.keys(body).sort()).toEqual(["name", "use_mimalloc"]);
        enabled = body.use_mimalloc;
        mutations.push(route.request().url());
        await route.fulfill({json: server()});
    });
    page.on("request", request => {
        if (request.method() === "POST" && /\/servers?\/(?:\d+\/)?(?:start|stop|kill)$/.test(new URL(request.url()).pathname)) {
            mutations.push(request.url());
        }
    });
    await page.goto("/servers");
    await page.getByRole("button", {name: "Edit", exact: true}).click();
    await expect(page.getByText("Saved for the next server start.", {exact: false})).toBeVisible();
    await page.getByRole("checkbox", {name: "Use mimalloc memory allocator (Linux only)"}).check();
    await page.getByRole("button", {name: "Save Changes", exact: true}).click();
    await expect.poll(() => enabled).toBe(true);
    await navigate(page, "Overview");
    await navigate(page, "Servers");
    await page.reload();
    await page.getByRole("button", {name: "Edit", exact: true}).click();
    await expect(page.getByRole("checkbox", {name: "Use mimalloc memory allocator (Linux only)"})).toBeChecked();
    expect(mutations).toHaveLength(1);
    await expect(page.getByRole("button", {name: "Stop", exact: true})).toBeVisible();
});

test("reusable presets cannot be selected as legacy world snapshots", async ({page}) => {
    await page.route("**/api/profiles", async route => {
        const response = await route.fetch();
        const state = await response.json();
        state.profiles.push({id: "preset-only", name: "Reusable test preset", is_global: true, preset_only: true, active: false, save_count: 0, mod_count: 0, installed_version: "2.0.77", game_mode: "factorio"});
        await route.fulfill({response, json: state});
    });
    await page.goto("/profiles");
    const card = page.locator("article").filter({has: page.getByRole("heading", {name: "Reusable test preset", exact: true})});
    await expect(card.getByText("Reusable preset", {exact: true})).toBeVisible();
    await expect(card.getByRole("button", {name: "Activate", exact: true})).toHaveCount(0);
    const picker = page.getByRole("button", {name: /Active profile/});
    await picker.click();
    await expect(page.locator("#profile-switch-menu").getByRole("button", {name: /Reusable test preset/})).toHaveCount(0);
    await picker.click();
    await card.getByRole("button", {name: "Use on a server", exact: true}).click();
    await expect(page.getByRole("heading", {name: "Servers", exact: true})).toBeVisible();
    await expect(page.locator('[aria-label="Factorio process controls"]')).toHaveCount(0);
});

test("preset capture targets the selected stopped server instead of the running default game", async ({page}) => {
    await page.setExtraHTTPHeaders({"x-fsm-test-scenario": "running"});
    let captured = false;
    await page.route("**/api/servers", route => route.fulfill({json: [
        {id: "1", name: "Running source", running: true, port: 34197},
        {id: "2", name: "Stopped source", running: false, port: 34198}
    ]}));
    await page.route("**/api/profiles", async route => {
        const response = await route.fetch();
        const state = await response.json();
        if (captured) state.profiles.push({id: "captured", name: "From server two", is_global: true});
        await route.fulfill({response, json: state});
    });
    await page.route("**/api/servers/2/profiles", async route => {
        expect(route.request().method()).toBe("POST");
        expect(route.request().postDataJSON()).toEqual({name: "From server two", description: ""});
        captured = true;
        await route.fulfill({json: {profiles: [{id: "captured", name: "From server two", is_global: true}]}});
    });
    await page.goto("/servers");
    await page.getByRole("button", {name: "Global Profiles", exact: true}).click();
    await page.getByRole("button", {name: "Capture Preset", exact: true}).click();
    await expect(page.getByRole("button", {name: "Create preset", exact: true})).toBeDisabled();
    await page.getByLabel("Source server", {exact: true}).selectOption("2");
    await page.getByLabel("Name", {exact: true}).fill("From server two");
    await page.getByRole("button", {name: "Create preset", exact: true}).click();
    await expect.poll(() => captured).toBe(true);
    await expect(page.getByText("From server two", {exact: true})).toBeVisible();
});

test("every authenticated page opens directly with its own lazy assets", async ({page}) => {
    for (const [url, title] of routes) {
        const response = await page.goto(url);
        expect(response.headers()["cache-control"]).toContain("must-revalidate");
        await expect(page.getByRole("heading", {level: 1, name: title, exact: true})).toBeVisible();
        await expect(page.getByRole("status", {name: "Loading page…"})).toHaveCount(0);
        await expect(page.getByText("This page could not be loaded.", {exact: false})).toHaveCount(0);
        const layout = await page.evaluate(() => ({
            width: document.documentElement.scrollWidth, viewport: window.innerWidth,
            overflow: [...document.querySelectorAll("main *")].filter(el => el.getBoundingClientRect().right > window.innerWidth + 1)
                .slice(0, 8).map(el => ({tag: el.tagName, class: el.className, right: el.getBoundingClientRect().right}))
        }));
        expect(layout.width, `${url}: ${JSON.stringify(layout)}`).toBeLessThanOrEqual(layout.viewport);
        await expect.poll(() => page.locator(".ui-brand-mark img").first().evaluate(img => img.complete && img.naturalWidth > 0)).toBe(true);
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
    await expect(page.locator(".ui-profile-switcher__icon")).toHaveCSS("color", "rgb(244, 163, 64)");
    await page.goBack();
    await expect(page.getByRole("heading", {level: 1, name: "Overview"})).toBeVisible();
});

test("help text remains inside the viewport when focused on a narrow screen", async ({page}) => {
    await page.goto("/mods");
    await expect(page.getByText("No mods in this set", {exact: true})).toBeVisible();
    const help = page.getByRole("button", {name: "Mods & mod packs help"});
    await help.focus();
    const tooltip = page.getByRole("tooltip");
    await expect(tooltip).toBeVisible();
    const bounds = await tooltip.boundingBox();
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(page.viewportSize().width);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
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
    await disableNativePointerFocus(page);
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

test("a dismissed mod-pack dialog restores focus without native pointer focus", async ({page}) => {
    await disableNativePointerFocus(page);
    await page.goto("/mods");
    const create = page.getByRole("button", {name: "Save current mod set", exact: true});
    await create.click();
    const dialog = page.getByRole("dialog", {name: "Create mod pack"});
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(create).toBeFocused();
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
    await page.route("**/api/log/tail**", route => route.fulfill({status: 503, contentType: "text/plain", body: "Unavailable"}));
    await page.goto("/logs");
    await expect(page.getByRole("alert")).toContainText("Factorio logs could not be loaded");
    await page.unroute("**/api/log/tail**");
    await page.getByRole("button", {name: "Refresh", exact: true}).click();
    await expect(page.getByText("Synthetic test log", {exact: false})).toBeVisible();
});

test("profile picker works from the keyboard and links to new profile setup", async ({page}) => {
    await disableNativePointerFocus(page);
    await page.goto("/");
    await expect(page.getByRole("heading", {level: 1, name: "Overview"})).toBeVisible();
    const openNavigation = page.getByRole("button", {name: "Open navigation", exact: true});
    const compact = await openNavigation.isVisible();
    if (compact) {
        await openNavigation.click();
        await expect(page.getByRole("button", {name: "Close navigation", exact: true})).toBeFocused();
    }
    const picker = page.getByRole("button", {name: /Active profile \(Server 1 snapshot\) Demo factory/});
    await picker.focus();
    await page.keyboard.press("Enter");
    await expect(picker).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");
    await expect(picker).toHaveAttribute("aria-expanded", "false");
    await expect(picker).toBeFocused();
    if (compact) {
        await page.keyboard.press("Escape");
        await expect(openNavigation).toBeFocused();
        await openNavigation.click();
        await expect(page.getByRole("button", {name: "Close navigation", exact: true})).toBeFocused();
    }
    await picker.focus();
    await page.keyboard.press("Enter");
    await expect(picker).toHaveAttribute("aria-expanded", "true");
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
