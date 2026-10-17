#!/usr/bin/env python3
"""Real-engine integration on owned, disposable Docker volumes; no user fixtures."""

import argparse
import http.cookiejar
import io
import json
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid
import zipfile


LABEL = "io.factorio-server-control.quality-test"
MOD = "fsm-quality-fixture"
MOD_FILE = MOD + "_1.0.0.zip"
SETTING = "fsm-quality-number"


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def fixture_mod(version):
    """Original test-only Lua, not copied from a game or third-party mod."""
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
        root = MOD + "_1.0.0/"
        archive.writestr(root + "info.json", json.dumps({
            "name": MOD, "version": "1.0.0", "title": "Disposable quality fixture",
            "author": "FSM tests", "factorio_version": ".".join(version.split(".")[:2]),
            "dependencies": ["base >= " + version],
        }))
        archive.writestr(root + "settings.lua", 'data:extend({{type="int-setting",'
                         'name="' + SETTING + '",setting_type="startup",'
                         'default_value=7,minimum_value=1,maximum_value=20}})\n')
    return output.getvalue()


def multipart(field, filename, data):
    boundary = "fsm-" + uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="{field}"; '
            f'filename="{filename}"\r\nContent-Type: application/zip\r\n\r\n').encode()
    return body + data + f"\r\n--{boundary}--\r\n".encode(), "multipart/form-data; boundary=" + boundary


def owned_resource(kind, name, token, metadata):
    expected = "fsm-quality-" + token
    name_ok = name == expected if kind == "container" else name.startswith(expected + "-")
    labels = metadata.get("Config", {}).get("Labels") if kind == "container" else metadata.get("Labels")
    return bool(re.fullmatch(r"[a-f0-9]{32}", token) and name_ok and labels and labels.get(LABEL) == token)


class HTTPFailure(RuntimeError):
    def __init__(self, method, path, status):
        self.status = status
        super().__init__(f"{method} {path}: HTTP {status} (response body withheld)")


class DisposableManager:
    def __init__(self, image, version, layout):
        self.image, self.version, self.layout = image, version, layout
        self.token = uuid.uuid4().hex
        self.name = "fsm-quality-" + self.token
        self.volumes = []
        self.base = None
        self.opener = None

    @staticmethod
    def docker(*args, timeout=240, optional=False):
        result = subprocess.run(["docker", *args], capture_output=True, timeout=timeout, check=False)
        if result.returncode and not optional:
            # Never print exec output, credentials, container logs or environment.
            raise RuntimeError(f"docker {args[0]} failed with exit {result.returncode}; output withheld")
        return result.stdout if result.returncode == 0 else None

    def inspect(self, kind, name):
        raw = self.docker(kind, "inspect", name, optional=True)
        return json.loads(raw)[0] if raw is not None else None

    def require_owned(self, kind, name):
        metadata = self.inspect(kind, name)
        check(metadata is not None and owned_resource(kind, name, self.token, metadata),
              f"Refusing to change an unowned {kind}: {name}")

    def create_volumes(self):
        targets = {"manager": "/opt/fsm-data"}
        if self.layout == "combined":
            targets["factorio"] = "/opt/factorio"
        else:
            targets.update({item: "/opt/factorio/" + item for item in ("saves", "mods", "config")})
        for suffix, target in targets.items():
            name = self.name + "-" + suffix
            check(self.inspect("volume", name) is None, "Unexpected pre-existing test volume")
            self.docker("volume", "create", "--label", LABEL + "=" + self.token, name)
            self.volumes.append((name, target))
            self.require_owned("volume", name)

    def create_container(self, requested_version):
        check(self.inspect("container", self.name) is None, "Unexpected pre-existing test container")
        args = ["run", "--detach", "--name", self.name, "--label", LABEL + "=" + self.token,
                "--publish", "127.0.0.1::80", "--stop-timeout", "90",
                "--env", "FSM_COOKIE_SECURE=false", "--env", "FACTORIO_VERSION=" + requested_version]
        for name, target in self.volumes:
            self.require_owned("volume", name)
            args += ["--mount", f"type=volume,source={name},target={target}"]
        self.docker(*args, self.image)
        self.require_owned("container", self.name)
        address = self.docker("port", self.name, "80/tcp").decode().strip()
        check(re.fullmatch(r"127\.0\.0\.1:[0-9]+", address), "Manager must bind only to loopback")
        self.base = "http://" + address
        self.wait_http()

    def wait_http(self):
        def ready():
            try:
                with urllib.request.urlopen(self.base + "/", timeout=5) as response:
                    return response.status == 200
            except (urllib.error.URLError, TimeoutError, ConnectionError):
                return False
        self.wait_for(ready, "manager startup/download", 360)

    @staticmethod
    def wait_for(condition, description, timeout=90):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if condition():
                return
            time.sleep(1)
        raise TimeoutError("Timed out waiting for " + description)

    def login(self, expected_credential=None):
        self.require_owned("container", self.name)
        credential = self.docker("exec", self.name, "cat", "/opt/fsm-data/initial-admin-password.txt")
        if expected_credential is not None:
            check(credential == expected_credential, "Administrator credential changed across restart/recreation")
        fields = dict(line.split("=", 1) for line in credential.decode().splitlines() if "=" in line)
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.api("POST", "/login", {"username": fields["username"], "password": fields["password"]})
        return credential

    def api(self, method, path, payload=None, raw=False, upload=None):
        body, headers = None, {}
        if payload is not None:
            body, headers = json.dumps(payload).encode(), {"Content-Type": "application/json"}
        if upload:
            body, content_type = multipart(*upload)
            headers = {"Content-Type": content_type}
        request = urllib.request.Request(self.base + "/api" + path, data=body, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=180) as response:
                data = response.read(64 * 1024 * 1024 + 1)
                check(len(data) <= 64 * 1024 * 1024, "Unexpectedly large fixture response")
                return data if raw else json.loads(data) if data else None
        except urllib.error.HTTPError as error:
            raise HTTPFailure(method, path, error.code) from None

    def reject(self, method, path, status, payload=None, upload=None):
        try:
            self.api(method, path, payload, upload=upload)
        except HTTPFailure as error:
            check(error.status == status, str(error))
        else:
            raise AssertionError(f"{method} {path} unexpectedly succeeded")

    def stop(self):
        self.api("POST", "/server/stop")
        self.wait_for(lambda: not any(self.api("GET", "/server/status")[key] for key in ("running", "stopping")), "game stop")

    def wait_game(self):
        self.wait_for(lambda: self.api("GET", "/server/players")["live_available"], "live game RCON")

    def active_profile(self):
        state = self.api("GET", "/profiles")
        return next(profile for profile in state["profiles"] if profile["id"] == state["active_profile_id"])

    def mod_value(self):
        view = self.api("GET", "/mods/startup-settings")
        setting = next(item for group in view["groups"] for item in group["settings"] if item["name"] == SETTING)
        return view, setting["value"]

    def remove_container(self):
        if self.inspect("container", self.name) is not None:
            self.require_owned("container", self.name)
            self.docker("stop", "--time", "90", self.name)
            self.docker("rm", self.name)

    def cleanup(self):
        self.remove_container()
        for name, _ in reversed(self.volumes):
            self.require_owned("volume", name)
            self.docker("volume", "rm", name)


def run_scenario(manager, mode):
    print(f"Testing {manager.version}, {mode}, {manager.layout} mounts", flush=True)
    manager.create_volumes()
    manager.create_container(manager.version)
    credential = manager.login()
    api = manager.api
    check(api("GET", "/server/facVersion")["version"] == manager.version, "Wrong initial engine version")
    api("PUT", "/map-snapshot/settings", {"enabled": False, "interval_minutes": 60})
    api("POST", "/server/game-mode", {"mode": mode})
    settings = api("GET", "/settings")
    settings.update({"name": "Disposable quality test", "visibility": {"public": False, "lan": False},
                     "require_user_verification": False, "auto_pause": False, "autosave_interval": 0})
    api("POST", "/settings/update", settings)
    mod_archive = fixture_mod(manager.version)
    api("POST", "/mods/upload", upload=("mod_file", MOD_FILE, mod_archive))
    view, value = manager.mod_value()
    check(value == 7, "Fixture startup setting was not discovered")
    api("PATCH", "/mods/startup-settings", {"revision": view["revision"], "changes": [{"name": SETTING, "value": 11}]})
    check(manager.mod_value()[1] == 11, "Startup setting was not persisted")
    print("Synthetic mod upload and engine-evaluated startup setting: OK", flush=True)

    options = api("GET", "/saves/generation/options")
    check(options["game_mode"] == mode, "Wrong generation game mode")
    check(len(options["planets"]) == (5 if mode == "space-age" else 1), "Wrong generation planet list")
    world = {"name": "quality-world.zip", "preset": "default", "seed": 42,
             "width": 128, "height": 128, "no_enemies_mode": True}
    check(api("POST", "/saves/generation/create", world)["name"] == world["name"], "World generation failed")
    original = api("GET", "/saves/dl/quality-world.zip", raw=True)
    with zipfile.ZipFile(io.BytesIO(original)) as archive:
        check(archive.testzip() is None, "Engine produced an invalid save")
    api("POST", "/saves/upload", upload=("savefile", "quality-upload.zip", original))
    check(api("GET", "/saves/dl/quality-upload.zip", raw=True) == original, "Save upload changed bytes")
    # No Mod Portal account is embedded in CI. Rejection must preserve current mods/mode/save.
    before_mods = api("GET", "/mods/download", raw=True)
    manager.reject("POST", "/saves/mods/import", 422, {"saveFile": "quality-upload.zip"})
    check(api("GET", "/mods/download", raw=True) == before_mods, "Unauthenticated import changed mods")
    check(api("GET", "/server/game-mode")["mode"] == mode, "Rejected import changed game mode")
    check(api("GET", "/saves/dl/quality-upload.zip", raw=True) == original, "Rejected import changed save")
    print("World creation, save import and failed mod-import preservation: OK", flush=True)

    api("POST", "/server/start", {"bindip": "0.0.0.0", "port": 34197, "savefile": "quality-upload.zip"})
    manager.wait_game()
    manager.reject("DELETE", "/saves/rm/quality-upload.zip", 423)
    manager.reject("POST", "/saves/upload", 423, upload=("savefile", "blocked.zip", original))
    checkpoint = api("POST", "/checkpoints")["checkpoints"][0]
    check(checkpoint["trigger"] == "manual", "Expected a live manual checkpoint")
    checkpoint_path = "/checkpoints/" + checkpoint["id"]
    checkpoint_bytes = api("GET", checkpoint_path + "/download", raw=True)
    manager.stop()
    restored = api("POST", checkpoint_path + "/restore")["name"]
    check(api("GET", "/saves/dl/" + restored, raw=True) == checkpoint_bytes, "Checkpoint restore changed bytes")
    check(api("GET", checkpoint_path + "/download", raw=True) == checkpoint_bytes, "Restore changed fixed checkpoint")
    api("PUT", "/server/autostart", {"enabled": True})
    profile_id = manager.active_profile()["id"]
    api("POST", "/server/start", {"bindip": "0.0.0.0", "port": 34197, "savefile": restored})
    manager.wait_game()
    print("Live checkpoint, restore, running-state guards and autostart setup: OK", flush=True)

    for operation in ("restart", "recreate"):
        if operation == "restart":
            manager.require_owned("container", manager.name)
            manager.docker("restart", "--time", "90", manager.name)
            manager.wait_http()
        else:
            manager.remove_container()
            # A changed rolling first-install default must not advance persisted exact runtime state.
            manager.create_container("stable")
        manager.login(credential)
        manager.wait_game()
        check(api("GET", "/server/status")["savefile"] == restored, "Selected save changed")
        check(api("GET", "/server/facVersion")["version"] == manager.version, "Persisted exact version changed")
        check(manager.active_profile()["id"] == profile_id, "Active profile changed")
        check(api("GET", "/server/game-mode")["mode"] == mode, "Game mode changed")
        check(api("GET", "/server/autostart")["enabled"], "Autostart preference changed")
        check(api("GET", "/saves/dl/quality-world.zip", raw=True) == original, "Unused original save changed")
        check(api("GET", checkpoint_path + "/download", raw=True) == checkpoint_bytes, "Fixed checkpoint changed")
        with zipfile.ZipFile(io.BytesIO(api("GET", "/mods/download", raw=True))) as archive:
            check(archive.read(MOD_FILE) == mod_archive, "Mod ZIP changed")
        manager.stop()
        check(manager.mod_value()[1] == 11, "Mod startup setting changed")
        if operation == "restart":
            api("POST", "/server/start", {"bindip": "0.0.0.0", "port": 34197, "savefile": restored})
            manager.wait_game()
        print(f"Container {operation}, autostart and persistent-state assertions: OK", flush=True)


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--disposable", action="store_true", required=True,
                        help="Acknowledge creation and removal of owned test containers/volumes")
    parser.add_argument("--image", default="factorio-server-manager:ci")
    parser.add_argument("--version", required=True, help="Exact official Factorio 2.x version")
    parser.add_argument("--mode", choices=("factorio", "space-age"), required=True)
    parser.add_argument("--layout", choices=("combined", "split"), required=True)
    args = parser.parse_args(argv)
    if not re.fullmatch(r"2\.[0-9]+\.[0-9]+", args.version):
        parser.error("--version must be an exact Factorio 2.x version, not a rolling channel")
    if not re.fullmatch(r"[a-z0-9][a-zA-Z0-9./:_@-]*", args.image):
        parser.error("invalid Docker image reference")
    return args


if __name__ == "__main__":
    args = parse_args()
    manager = DisposableManager(args.image, args.version, args.layout)
    try:
        run_scenario(manager, args.mode)
    finally:
        manager.cleanup()
