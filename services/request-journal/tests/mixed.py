"""Mixed HTTP correctness workload for a fresh, disposable Request Journal DB."""
import concurrent.futures
from contextlib import closing
import http.client
import json
import socket
import sqlite3
import threading
import time
import urllib.parse


def exercise(request, base, database, workers=16, rounds=30):
    shared = request("POST", "/requests", {"title": "Shared contention"}, 201)
    shared_path = "/requests/" + str(shared["id"])
    survivors = []
    deleted = []
    count = 1
    lock = threading.Lock()
    gate = threading.Barrier(workers)

    def call(*args, **kwargs):
        nonlocal count
        result = request(*args, **kwargs)
        with lock:
            count += 1
        return result

    def worker(worker_id):
        kept, removed = [], []
        gate.wait(timeout=15)
        for index in range(rounds):
            title = f"mixed-{worker_id}-{index}"
            item = call("POST", "/requests", {"title": title}, 201)
            path = "/requests/" + str(item["id"])
            call("PUT", path, {"title": title, "status": "in_progress"})
            assert call("GET", path)["status"] == "in_progress"
            assert isinstance(call("GET", "/requests?status=open&limit=5"), list)
            assert [event["status"] for event in call("GET", path + "/history")] == ["open", "in_progress"]
            resolved = call("PUT", path, {"title": title, "status": "resolved"})
            history = call("GET", path + "/history")
            assert [event["status"] for event in history] == ["open", "in_progress", "resolved"]
            call("PUT", shared_path, {"title": title, "status": ("open", "in_progress", "resolved")[(worker_id + index) % 3]})
            if index % 3:
                call("DELETE", path, expected=204)
                call("GET", path, expected=404)
                removed.append(item["id"])
            else:
                kept.append((path, resolved, history))
        return kept, removed

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        for kept, removed in pool.map(worker, range(workers)):
            survivors.extend(kept)
            deleted.extend(removed)
    latest = call("GET", shared_path)
    history = call("GET", shared_path + "/history")
    statuses = [event["status"] for event in history]
    assert statuses[0] == "open" and statuses[-1] == latest["status"]
    assert all(left != right for left, right in zip(statuses, statuses[1:])), "duplicate status event under contention"
    event_ids = [event["id"] for event in history]
    assert event_ids == sorted(set(event_ids)), "history order/identity changed"
    assert 2 <= len(history) <= workers * rounds + 1
    survivors.append((shared_path, latest, history))

    # A second SQLite writer holds the write lock. The HTTP writer must wait
    # and then succeed, rather than leak SQLITE_BUSY or lose its history.
    with closing(sqlite3.connect(database, timeout=10)) as blocker:
        blocker.execute("BEGIN IMMEDIATE")
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(call, "POST", "/requests", {"title": "waited-for-lock"}, 201)
            try:
                time.sleep(.25)
                assert not pending.done(), "writer completed while another transaction held the lock"
            finally:
                blocker.rollback()
            item = pending.result(timeout=10)
    path = "/requests/" + str(item["id"])
    history = call("GET", path + "/history")
    assert [event["status"] for event in history] == ["open"]
    survivors.append((path, item, history))

    # Keep the DB busy while a fully sent HTTP write is disconnected. After
    # releasing the lock, readiness must recover and the cancelled write must
    # not appear. Repeat with distinct titles to detect delayed writes as well.
    address = urllib.parse.urlsplit(base)
    cancelled_titles = []
    for number in range(3):
        title = f"cancelled-behind-lock-{number}"
        cancelled_titles.append(title)
        with closing(sqlite3.connect(database, timeout=10)) as blocker:
            blocker.execute("BEGIN IMMEDIATE")
            connection = http.client.HTTPConnection(address.hostname, address.port, timeout=10)
            try:
                connection.request("POST", "/requests", json.dumps({"title": title}), {"Content-Type": "application/json"})
                time.sleep(.25)
                connection.sock.shutdown(socket.SHUT_RDWR)
                connection.close()
                time.sleep(.25)
            finally:
                connection.close()
                blocker.rollback()
        assert call("GET", "/health") == {"status": "ok"}
    with closing(sqlite3.connect(database, timeout=10)) as db:
        assert db.execute("PRAGMA integrity_check").fetchone() == ("ok",)
        assert db.execute("PRAGMA foreign_key_check").fetchall() == []
        for identity in deleted:
            assert db.execute("SELECT COUNT(*) FROM requests WHERE id=?", (identity,)).fetchone()[0] == 0
            assert db.execute("SELECT COUNT(*) FROM request_events WHERE request_id=?", (identity,)).fetchone()[0] == 0
        for title in cancelled_titles:
            assert db.execute("SELECT COUNT(*) FROM requests WHERE title=?", (title,)).fetchone()[0] == 0, title
        assert db.execute("SELECT COUNT(*) FROM request_events e LEFT JOIN requests r ON r.id=e.request_id WHERE r.id IS NULL").fetchone()[0] == 0
    print(f"PASS: mixed workload, {workers} workers x {rounds} lifecycles, {count} verified HTTP responses, "
          f"{len(deleted)} cascading deletes, {len(survivors)} persisted records, shared-row history, "
          "SQLite write-lock recovery, no disconnected writes persisted, DB integrity", flush=True)
    return survivors


def verify_persisted(request, survivors):
    for path, item, history in survivors:
        assert request("GET", path) == item
        assert request("GET", path + "/history") == history
    print(f"PASS: all {len(survivors)} mixed-workload records and histories survived restart", flush=True)
