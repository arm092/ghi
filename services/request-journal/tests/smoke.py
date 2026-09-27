"""Fresh consumer install, Ghi tests and real HTTP/restart checks (Python 3)."""
import argparse
import concurrent.futures
from contextlib import closing
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ghi", default="ghi")
    parser.add_argument("--mojave", default="mojave")
    args = parser.parse_args()
    source = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="ghi-journal-consumer-") as temporary:
        root = Path(temporary) / "request-journal"
        shutil.copytree(source, root, ignore=shutil.ignore_patterns(".ghi", "bin", "*.db*", "__pycache__"))
        def run(*command):
            subprocess.run(command, cwd=root, check=True, timeout=300)

        run(args.ghi, "version")
        run(args.mojave, "version")
        locked = (root / "mojave.lock").read_bytes()
        run(args.mojave, "install")
        assert (root / "mojave.lock").read_bytes() == locked, "install changed lock"
        run(args.ghi, "test", ".")
        executable = root / "bin" / ("journal.exe" if os.name == "nt" else "journal")
        run(args.ghi, "build", "-o", str(executable), ".")
        environment = dict(os.environ, JOURNAL_ADDR="127.0.0.1:0", JOURNAL_DB=str(root / "journal.db"), JOURNAL_MIGRATIONS=str(root / "migrations"), JOURNAL_SHUTDOWN_TIMEOUT="5s")
        # Configuration must fail before opening the database or a listener,
        # and typed parse errors must not disclose the supplied value.
        for value in ("", "0s", "-1s", "private-invalid-duration"):
            bad_env = dict(environment, JOURNAL_SHUTDOWN_TIMEOUT=value)
            failed = subprocess.run([str(executable)], cwd=root, env=bad_env, capture_output=True, text=True, timeout=30)
            output = failed.stdout + failed.stderr
            assert failed.returncode != 0 and "JOURNAL_SHUTDOWN_TIMEOUT" in output
            assert "private-invalid-duration" not in output and '"msg":"listening"' not in output
            assert not (root / "journal.db").exists(), "invalid config created the database"
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        process = None
        log = None
        base = ""

        def stop():
            nonlocal process, log
            if process is not None:
                if process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
                process = None
            if log is not None:
                log.close()
                log = None

        def start():
            nonlocal process, log, base
            path = root / "server.log"
            log = path.open("wb")
            process = subprocess.Popen([str(executable)], cwd=root, env=environment, stdout=log, stderr=subprocess.STDOUT)
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                for line in path.read_text(errors="replace").splitlines():
                    try:
                        event = json.loads(line)
                    except ValueError:
                        continue
                    if event.get("msg") == "listening":
                        base = "http://" + event["address"]
                        return
                if process.poll() is not None:
                    raise AssertionError(path.read_text())
                time.sleep(.03)
            raise AssertionError("server startup timed out: " + path.read_text())

        def request(method, path, body=None, expected=200, raw=False):
            data = body.encode() if raw else (json.dumps(body).encode() if body is not None else None)
            req = urllib.request.Request(base + path, data=data, method=method, headers={"Content-Type": "application/json"})
            try:
                response = opener.open(req, timeout=10)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                content = response.read()
                assert response.status == expected, (method, path, response.status, content)
                if expected == 405:
                    assert response.headers.get("Allow") == "GET, PUT, DELETE"
                if content:
                    assert response.headers.get_content_type() == "application/json"
                    return json.loads(content)
                return None

        try:
            start()
            assert request("GET", "/health") == {"status": "ok"}
            assert request("GET", "/requests") == []
            item = request("POST", "/requests", {"title": "  Restore notification delivery  "}, 201)
            identity = "/requests/" + str(item["id"])
            assert item["title"] == "Restore notification delivery" and item["status"] == "open"
            assert request("GET", identity) == item
            changed = request("PUT", identity, {"title": item["title"], "status": "in_progress"})
            assert changed["created_at"] == item["created_at"]
            request("PUT", identity, {"title": "Notifications restored", "status": "resolved"})
            request("PUT", identity, {"title": "Notifications restored", "status": "resolved"})
            history = request("GET", identity + "/history")
            assert [entry["status"] for entry in history] == ["open", "in_progress", "resolved"]
            assert len(request("GET", "/requests?status=resolved")) == 1
            assert request("GET", "/requests?status=open") == []
            for payload in ({"title": "\u0000hello"}, {"title": " "}, {"title": "x" * 201}, {"title": "x", "status": "resolved"}):
                request("POST", "/requests", payload, 422)
            for payload in ('{"title":"x","unknown":1}', '{"title":"x"} {}', '{', '"text"', '{"title":"' + 'x' * 5000 + '"}'):
                request("POST", "/requests", payload, 400, raw=True)
            saved_item = request("GET", identity)
            saved_history = request("GET", identity + "/history")
            request("PUT", identity, {"title": "x", "status": "missing"}, 422)
            invalid = request("PUT", identity, {"title": " ", "status": "missing"}, 422)
            assert invalid["fields"] == [
                {"field": "title", "code": "required"},
                {"field": "title", "code": "min_length"},
                {"field": "status", "code": "one_of"},
            ]
            invalid = request("GET", "/requests?limit=0&offset=-1&status=missing", expected=422)
            assert invalid["fields"] == [
                {"field": "status", "code": "one_of"},
                {"field": "limit", "code": "min_value"},
                {"field": "offset", "code": "min_value"},
            ]
            invalid = request("POST", "/requests", {"title": "\u0000hello"}, 422)
            assert invalid["fields"] == [{"field": "title", "code": "nul_character"}]
            unicode_item = request("POST", "/requests", {"title": "Ղ" * 200}, 201)
            request("POST", "/requests", {"title": "Ղ" * 201}, 422)
            request("DELETE", "/requests/" + str(unicode_item["id"]), expected=204)
            assert request("GET", identity) == saved_item, "invalid update changed storage"
            assert request("GET", identity + "/history") == saved_history, "invalid update changed history"
            invalid = request("POST", "/requests", {"title": " ", "status": "resolved"}, 422)
            assert invalid["fields"] == [
                {"field": "title", "code": "required"},
                {"field": "title", "code": "min_length"},
                {"field": "status", "code": "initial_status"},
            ]
            invalid = request("GET", "/requests?limit=abc", expected=422)
            assert invalid["fields"] == [{"field": "limit", "code": "integer"}]
            for query in ("limit=0", "limit=101", "offset=-1", "limit=abc", "status=missing"):
                request("GET", "/requests?" + query, expected=422)
            invalid = request("GET", "/requests/not-an-id", expected=422)
            assert invalid["fields"] == [{"field": "id", "code": "positive_integer"}]
            request("GET", "/requests/999999", expected=404)
            request("PUT", "/requests/999999", {"title": "x", "status": "open"}, 404)
            request("GET", "/requests/999999/history", expected=404)
            request("DELETE", "/requests/999999", expected=404)
            # Repeated early rejections detect unread-body TCP resets on Windows.
            for _ in range(100):
                request("PATCH", identity, {}, 405)
                request("POST", "/missing", {}, 404)
            request("GET", "/missing", expected=404)
            injection = request("POST", "/requests", {"title": "'); DROP TABLE requests; --"}, 201)
            assert request("GET", "/requests/" + str(injection["id"]))["title"] == injection["title"]
            with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
                results = list(pool.map(lambda n: request("POST", "/requests", {"title": "Concurrent " + str(n)}, 201), range(12)))
            assert len({record["id"] for record in results}) == 12
            assert len(request("GET", "/requests?limit=2&offset=1")) == 2
            stop()
            start()
            assert request("GET", identity)["status"] == "resolved"
            assert request("GET", identity + "/history") == history
            request("DELETE", identity, expected=204)
            request("GET", identity, expected=404)
            stop()
            with closing(sqlite3.connect(root / "journal.db")) as database:
                assert database.execute("SELECT COUNT(*) FROM schema_migrations").fetchone()[0] == 1
                assert database.execute("SELECT COUNT(*) FROM request_events WHERE request_id = ?", (item["id"],)).fetchone()[0] == 0
            # Startup must fail before opening a listener if a migration is invalid.
            (root / "migrations/002_invalid.sql").write_text("CREATE TABLE unfinished(id INTEGER); INSERT INTO absent VALUES(1);")
            failed = subprocess.run([str(executable)], cwd=root, env=environment, capture_output=True, text=True, timeout=30)
            assert failed.returncode != 0 and '"msg":"listening"' not in failed.stdout
            with closing(sqlite3.connect(root / "journal.db")) as database:
                assert database.execute("SELECT COUNT(*) FROM sqlite_master WHERE name='unfinished'").fetchone()[0] == 0
                assert database.execute("SELECT COUNT(*) FROM schema_migrations").fetchone()[0] == 1
            print("PASS: fresh install, transactional tests, HTTP CRUD, validation, concurrency, restart persistence and migration failure")
        except Exception:
            print((root / "server.log").read_text(errors="replace"), flush=True)
            raise
        finally:
            stop()


if __name__ == "__main__":
    main()
