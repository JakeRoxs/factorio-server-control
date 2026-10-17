// Loopback-only, synthetic test data. Never reads manager/Factorio state and
// never starts a game or sends a request to the public mod portal.
const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");
const root = path.resolve(__dirname, "../../app");
const pages = new Set(["/", "/login", "/saves", "/mods", "/server-settings", "/game-settings", "/console", "/logs", "/releases", "/profiles", "/user-management"]);
const profiles = {
    active_profile_id: "demo-main",
    profiles: [
        {id: "demo-main", active: true, name: "Demo factory", game_mode: "space-age", installed_version: "2.0.77", release_target: "latest", save_count: 1, mod_count: 0, selected_save: "demo.zip", bind_ip: "0.0.0.0", port: 34197},
        {id: "demo-other", active: false, name: "Vanilla test", game_mode: "factorio", installed_version: "2.0.77", release_target: "stable", save_count: 1, mod_count: 0, selected_save: "vanilla.zip", bind_ip: "0.0.0.0", port: 34197}
    ]
};
const api = {
    "/api/profiles": profiles,
    "/api/user/list": [{username: "demo-admin", role: "admin"}, {username: "demo-viewer", role: "viewer"}],
    "/api/saves/list": [{name: "demo.zip", size: 1234567, last_mod: "2026-01-01T12:00:00Z"}],
    "/api/checkpoints": {settings: {interval_enabled: false, interval_minutes: 30, last_player_enabled: false, clean_stop_enabled: false, retention_count: 10}, checkpoints: []},
    "/api/map-snapshot": {settings: {enabled: true, interval_minutes: 60, automatic_only_when_no_players: false, include_space_platforms: true}, running: false, snapshot: null},
    "/api/server/autostart": {enabled: false},
    "/api/server/facVersion": {fac_version: "2.0.77", base_mod_version: "2.0.77"},
    "/api/server/release/status": {installed_version: "2.0.77", installed_channel: "latest", installed_target: "latest"},
    "/api/server/game-mode": {mode: "space-age", space_age_available: true, features: []},
    "/api/settings": {name: "Demo server", description: "Synthetic browser test", max_players: 8, auto_pause: true, visibility: {public: false, lan: true}},
    "/api/config": {network: {port: 34197}, other: {autosave_interval: 10}},
    "/api/log/tail": ["Synthetic test log — no real game output"],
    "/api/mods/list": [],
    "/api/mods/packs/list": [],
    "/api/mods/portal/list": {factorio_version: "2.0", results: []},
    "/api/mods/portal/loginstatus": false,
    "/api/mods/portal/preferences": {preselect_optional: false},
    "/api/mods/startup-settings": {revision: "synthetic-revision", groups: []},
    "/api/backups/limits": {max_upload_bytes: 1073741824}
};

http.createServer((req, res) => {
    const url = new URL(req.url, "http://127.0.0.1:18089");
    const scenario = req.headers["x-fsm-test-scenario"] || "stopped";
    const json = (body, status = 200) => {
        res.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"});
        res.end(JSON.stringify(body));
    };
    if (url.pathname.startsWith("/api/")) {
        if (req.method !== "GET") return json("Synthetic operation failed; existing data was kept.", 409);
        if (url.pathname === "/api/user/status") return scenario === "anonymous"
            ? json("Authentication required", 401)
            : json({username: scenario === "viewer" ? "demo-viewer" : "demo-admin", role: scenario === "viewer" ? "viewer" : "admin"});
        if (url.pathname === "/api/server/status") return scenario === "unknown"
            ? json("Status unavailable", 503)
            : json({running: scenario === "running", stopping: false, savefile: "demo.zip", fac_version: "2.0.77"});
        if (url.pathname === "/api/server/players") return json({profile_id: "demo-main", server_running: scenario === "running", live_available: true, online_count: 0, online_players: [], players: []});
        if (Object.hasOwn(api, url.pathname)) return json(api[url.pathname]);
        return json(`Missing test fixture: ${req.method} ${url.pathname}`, 501);
    }
    let pathname;
    try { pathname = decodeURIComponent(url.pathname); }
    catch { res.writeHead(400).end(); return; }
    const file = pages.has(pathname) ? path.join(root, "index.html") : path.resolve(root, "." + pathname);
    if (!file.startsWith(root + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
        res.writeHead(404).end(); return;
    }
    const types = {".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".ico": "image/x-icon"};
    res.writeHead(200, {"Content-Type": types[path.extname(file)] || "application/octet-stream", "Cache-Control": "no-cache, max-age=0, must-revalidate", "X-Content-Type-Options": "nosniff"});
    fs.createReadStream(file).pipe(res);
}).listen(18089, "127.0.0.1", () => console.log("Synthetic FSM browser fixture on 127.0.0.1:18089"));
