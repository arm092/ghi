"""Build from public releases and verify Linux persistence and in-flight SIGTERM."""
import argparse
import http.client
import json
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import time
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", default="ghi-request-journal:0.2.7")
    parser.add_argument("--version", default="0.2.7")
    parser.add_argument("--skip-build", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]

    def docker(*command, timeout=120):
        return subprocess.check_output(["docker", *command], text=True, timeout=timeout).strip()

    if not args.skip_build:
        subprocess.run(["docker", "build", "--platform", "linux/amd64",
                        "--build-arg", "GHI_VERSION=" + args.version,
                        "-t", args.image, str(root)], check=True, timeout=1800)

    suffix = uuid.uuid4().hex[:12]
    volume = "ghi-journal-test-" + suffix
    container = "ghi-journal-test-" + suffix
    created = False
    docker("volume", "create", volume)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = ""

    def request(method, path, body=None, expected=200):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(base + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
        with opener.open(req, timeout=15) as response:
            assert response.status == expected
            return json.load(response)

    def start():
        nonlocal created, base
        docker("run", "-d", "--name", container, "--platform", "linux/amd64",
               "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid",
               "--mount", "type=volume,source=" + volume + ",target=/data",
               "-p", "127.0.0.1::8080", args.image)
        created = True
        binding = docker("port", container, "8080/tcp")
        port = int(binding.rsplit(":", 1)[1])
        base = "http://127.0.0.1:" + str(port)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                if request("GET", "/health") == {"status": "ok"}:
                    return port
            except (OSError, ValueError):
                pass
            time.sleep(.1)
        raise AssertionError("not ready: " + docker("logs", container))

    try:
        port = start()
        deadline = time.monotonic() + 20
        while docker("inspect", "-f", "{{.State.Health.Status}}", container) != "healthy":
            assert time.monotonic() < deadline, "container health check did not pass"
            time.sleep(.2)
        assert docker("exec", container, "id", "-u") == "10001"
        docker("exec", container, "sh", "-c",
               "test ! -e /usr/local/bin/ghi && test ! -e /usr/local/go/bin/go")
        item = request("POST", "/requests", {"title": "Persist across replacement"}, 201)
        identity = "/requests/" + str(item["id"])
        request("PUT", identity, {"title": item["title"], "status": "resolved"})
        history = request("GET", identity + "/history")

        # Keep a real handler active by streaming the body, then finish it while
        # Docker sends SIGTERM. Wait for HTTP refusal: Docker Desktop can keep
        # accepting TCP connections even after the application's listener closes.
        # Exit 0 plus HTTP201 then proves graceful draining.
        payload = json.dumps({"title": "Completed during shutdown"}).encode()
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
        connection.putrequest("POST", "/requests")
        connection.putheader("Content-Type", "application/json")
        connection.putheader("Content-Length", str(len(payload)))
        connection.endheaders()
        connection.send(payload[:5])
        time.sleep(.25)
        stopping = subprocess.Popen(["docker", "stop", "--time", "10", container],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        deadline = time.monotonic() + 5
        while True:
            probe = http.client.HTTPConnection("127.0.0.1", port, timeout=.5)
            try:
                probe.request("GET", "/health")
                probe.getresponse().read()
            except (OSError, http.client.HTTPException):
                break
            finally:
                probe.close()
            assert time.monotonic() < deadline, "listener did not close after SIGTERM"
            time.sleep(.05)
        connection.send(payload[5:])
        response = connection.getresponse()
        assert response.status == 201
        drained = json.loads(response.read())
        connection.close()
        stdout, stderr = stopping.communicate(timeout=15)
        assert stopping.returncode == 0, stdout + stderr
        assert docker("inspect", "-f", "{{.State.ExitCode}}", container) == "0"
        logs = docker("logs", container)
        assert "shutdown_failed" not in logs and "startup_failed" not in logs
        with tempfile.TemporaryDirectory(prefix="ghi-docker-db-") as temporary:
            snapshot = str(Path(temporary) / "journal.db")
            docker("cp", container + ":/data/journal.db", snapshot)
            database = sqlite3.connect(snapshot)
            try:
                assert database.execute("SELECT COUNT(*) FROM schema_migrations").fetchone()[0] == 1
                assert database.execute("SELECT COUNT(*) FROM requests").fetchone()[0] == 2
            finally:
                database.close()

        docker("rm", container)
        created = False
        start()
        assert request("GET", identity)["status"] == "resolved"
        assert request("GET", identity + "/history") == history
        assert request("GET", "/requests/" + str(drained["id"]))["title"] == drained["title"]
        docker("stop", "--time", "10", container)
        assert docker("inspect", "-f", "{{.State.ExitCode}}", container) == "0"
        print("PASS: container health, non-root read-only runtime, migrations, "
              "volume persistence, replacement restart and in-flight SIGTERM draining")
    finally:
        if created:
            subprocess.run(["docker", "rm", "-f", container], check=False, capture_output=True)
        subprocess.run(["docker", "volume", "rm", volume], check=False, capture_output=True)


if __name__ == "__main__":
    main()
