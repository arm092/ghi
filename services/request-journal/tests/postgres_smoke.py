"""Live PostgreSQL/HTTP acceptance in an isolated disposable Docker database."""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    args = parser.parse_args()
    executable = str(Path(args.binary).resolve())
    root = Path(__file__).resolve().parents[1]
    name = "ghi-journal-pg-" + uuid.uuid4().hex[:12]
    password = uuid.uuid4().hex
    process = None
    created = False
    def docker(*command):
        return subprocess.check_output(["docker", *command], text=True, timeout=120).strip()
    def sql(statement):
        return docker("exec", name, "psql", "-U", "journal", "-d", "journal", "-v", "ON_ERROR_STOP=1", "-tAc", statement)
    try:
        docker("run", "--rm", "-d", "--name", name, "-e", "POSTGRES_USER=journal",
               "-e", "POSTGRES_DB=journal", "-e", "POSTGRES_PASSWORD=" + password,
               "-p", "127.0.0.1::5432", "postgres:18-alpine")
        created = True
        deadline = time.monotonic() + 60
        while subprocess.run(["docker", "exec", name, "pg_isready", "-U", "journal"], capture_output=True).returncode:
            assert time.monotonic() < deadline, "PostgreSQL did not become ready"
            time.sleep(.2)
        port = docker("port", name, "5432/tcp").rsplit(":", 1)[1]
        environment = os.environ.copy()
        environment.update(JOURNAL_DB_DRIVER="postgres", JOURNAL_DB=f"postgres://journal:{password}@127.0.0.1:{port}/journal?sslmode=disable",
                           JOURNAL_ADDR="127.0.0.1:0", JOURNAL_MIGRATIONS=str(root/"migrations"))
        with tempfile.TemporaryDirectory(prefix="ghi-journal-pg-") as temporary:
            migrations = Path(temporary)/"migrations"
            shutil.copytree(root/"migrations", migrations)
            environment["JOURNAL_MIGRATIONS"] = str(migrations)
            logfile = Path(temporary)/"server.log"
            base = ""
            def stop():
                nonlocal process
                if process:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill(); process.wait(timeout=5)
                    process = None
            def start():
                nonlocal process, base
                with logfile.open("wb") as log:
                    process = subprocess.Popen([executable], cwd=root, env=environment, stdout=log, stderr=subprocess.STDOUT)
                deadline = time.monotonic()+30
                while time.monotonic()<deadline:
                    for line in logfile.read_text(errors="replace").splitlines():
                        event = json.loads(line)
                        if event.get("msg")=="listening":
                            base="http://"+event["address"]; return
                    assert process.poll() is None, logfile.read_text()
                    time.sleep(.05)
                raise AssertionError("service startup timed out")
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            def request(method, path, body=None, token="", expected=200):
                headers={"Content-Type":"application/json"}
                if token: headers["Authorization"]="Bearer "+token
                req=urllib.request.Request(base+path, data=None if body is None else json.dumps(body).encode(), method=method, headers=headers)
                try: response=opener.open(req, timeout=15)
                except urllib.error.HTTPError as error: response=error
                with response:
                    content=response.read()
                    assert response.status==expected, (path,response.status,content)
                    return json.loads(content) if content else None
            start()
            assert request("GET","/health")=={"status":"ok"}
            request("GET","/requests",expected=401)
            alice=request("POST","/auth/register",{"email":"alice@example.test","password":"a secure postgres password"},expected=201)
            bob=request("POST","/auth/register",{"email":"bob@example.test","password":"another postgres password"},expected=201)
            a,b=alice["token"],bob["token"]
            request("POST","/auth/register",{"email":"ALICE@example.test","password":"a secure postgres password"},expected=409)
            request("POST","/auth/login",{"email":"alice@example.test","password":"wrong password"},expected=401)
            login=request("POST","/auth/login",{"email":"alice@example.test","password":"a secure postgres password"})
            request("POST","/auth/logout",token=login["token"],expected=204)
            request("GET","/me",token=login["token"],expected=401)
            item=request("POST","/requests",{"title":"Postgres owned record"},a,201)
            path="/requests/"+str(item["id"])
            for method,suffix,body in [("GET","",None),("GET","/history",None),("PUT","",{"title":"stolen","status":"resolved"}),("DELETE","",None)]:
                request(method,path+suffix,body,b,404)
            assert request("GET","/requests",token=b)==[]
            # Concurrent updates serialize their read/status/history decision using FOR UPDATE.
            def change(index):
                return request("PUT",path,{"title":"Concurrent "+str(index),"status":("open","in_progress","resolved")[index%3]},a)
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
                list(pool.map(change,range(48)))
            saved=request("GET",path,token=a)
            history=request("GET",path+"/history",token=a)
            statuses=[event["status"] for event in history]
            assert statuses[-1]==saved["status"] and all(x!=y for x,y in zip(statuses,statuses[1:]))
            # Fail the event insert and require both title/status and history to roll back.
            sql("CREATE FUNCTION reject_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated'; END $$; CREATE TRIGGER reject_history BEFORE INSERT ON request_events FOR EACH ROW EXECUTE FUNCTION reject_event();")
            new_status="resolved" if saved["status"]!="resolved" else "open"
            assert request("PUT",path,{"title":"must roll back","status":new_status},a,500)=={"error":"internal server error"}
            assert request("GET",path,token=a)==saved and request("GET",path+"/history",token=a)==history
            sql("DROP TRIGGER reject_history ON request_events; DROP FUNCTION reject_event();")
            assert sql("SELECT COUNT(*) FROM schema_migrations")=="2"
            assert sql("SELECT COUNT(*) FROM sessions WHERE length(token_hash)<>64")=="0"
            stop(); start()
            assert request("GET",path,token=a)==saved and request("GET",path+"/history",token=a)==history
            request("DELETE",path,token=a,expected=204)
            assert sql("SELECT COUNT(*) FROM request_events")=="0"
            sql("UPDATE sessions SET expires_at=0")
            request("GET","/requests",token=a,expected=401)
            stop()
            (migrations/"postgres/003_invalid.sql").write_text("CREATE TABLE unfinished(id INTEGER); INSERT INTO absent VALUES(1);")
            failed = subprocess.run([executable], cwd=root, env=environment, capture_output=True, text=True, timeout=30)
            assert failed.returncode != 0 and '"msg":"listening"' not in failed.stdout
            assert sql("SELECT to_regclass('public.unfinished') IS NULL") == "t"
            assert sql("SELECT COUNT(*) FROM schema_migrations") == "2"
            print("PASS: PostgreSQL migrations, auth, isolation, concurrent history, atomic failure, restart sessions/data, expiry and cascade deletion")
    finally:
        if process:
            process.kill(); process.wait(timeout=10)
        if created: subprocess.run(["docker","rm","-f",name],capture_output=True,timeout=30)


if __name__=="__main__": main()
