"""Compare real DDD Ghi users routes with their read-only Go counterpart."""
from pathlib import Path
import argparse
import ctypes
import hashlib
import json
import os
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def peak_memory(pid):
    if os.name != 'nt':
        return None
    class Counters(ctypes.Structure):
        _fields_ = [('cb', ctypes.c_ulong), ('faults', ctypes.c_ulong)] + [(name, ctypes.c_size_t) for name in ('peak', 'working', 'pagePeak', 'page', 'nonPagePeak', 'nonPage', 'pagefile', 'pagefilePeak')]
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.OpenProcess.restype = ctypes.c_void_p
    kernel.CloseHandle.argtypes = [ctypes.c_void_p]
    psapi = ctypes.WinDLL('psapi')
    psapi.GetProcessMemoryInfo.argtypes = [ctypes.c_void_p, ctypes.POINTER(Counters), ctypes.c_ulong]
    handle = kernel.OpenProcess(0x410, False, pid)
    if not handle:
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        counters = Counters()
        counters.cb = ctypes.sizeof(counters)
        if not psapi.GetProcessMemoryInfo(handle, ctypes.byref(counters), counters.cb):
            raise ctypes.WinError(ctypes.get_last_error())
        return counters.peak
    finally:
        kernel.CloseHandle(handle)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--requests', type=int, default=20000)
    parser.add_argument('--runs', type=int, default=3)
    parser.add_argument('--baseline-ghi', type=Path, help='compare the current Ghi server with a prebuilt Ghi server instead of Go')
    parser.add_argument('--scenarios', nargs='+', choices=('page20', 'get', 'missing'), default=('page20', 'get', 'missing'))
    parser.add_argument('--output', default='.work/http-ddd-results.json')
    args = parser.parse_args()
    if args.requests < 100 or args.runs < 1:
        parser.error('use at least 100 requests and one run')
    root = Path(__file__).resolve().parents[2]
    (root / '.work').mkdir(exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix='http-ddd-', dir=root / '.work'))
    go = shutil.which('go')
    if not go:
        parser.error('Go must be available on PATH')
    env = dict(os.environ, GHI_GO=go, GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='', GOMAXPROCS='8')
    suffix = '.exe' if os.name == 'nt' else ''
    binaries = {key: work / (key + suffix) for key in ('compiler', 'Ghi', 'Go', 'client')}
    labels = {'Ghi': 'Ghi-after', 'Go': 'Ghi-before'} if args.baseline_ghi else {'Ghi': 'Ghi', 'Go': 'Go'}
    def run(command, cwd=root):
        return subprocess.check_output([str(x) for x in command], cwd=cwd, env=env, text=True, stderr=subprocess.STDOUT)
    locked = {module['path']: module['version'] for module in json.loads((root / 'examples/ddd-api/mojave.lock').read_text())['go']['modules']}
    for line in run([go, 'list', '-m', '-f', '{{.Path}} {{.Version}}', 'all'], root / 'benchmarks/http-api/go').splitlines():
        parts = line.split()
        if len(parts) == 2:
            assert locked.get(parts[0]) == parts[1], f'dependency version differs: {line}'
    run([go, 'build', '-o', binaries['compiler'], './cmd/ghi'])
    run([binaries['compiler'], 'build', '-o', binaries['Ghi'], root / 'examples/ddd-api'])
    if args.baseline_ghi:
        shutil.copyfile(args.baseline_ghi.resolve(), binaries['Go'])
    else:
        run([go, 'build', '-o', binaries['Go'], '.'], root / 'benchmarks/http-api/go')
    run([go, 'build', '-o', binaries['client'], './benchmarks/http-api/client'])
    template = work / 'template.db'
    migrations = root / 'examples/ddd-api/infrastructure/sqlite/migrations'
    def start(language, db, label):
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        log = (work / (label + '.log')).open('w')
        server_env = dict(env, GHI_DDD_ADDR=f'127.0.0.1:{port}', GHI_DDD_DB=str(db), GHI_DDD_MIGRATIONS=str(migrations), GHI_DDD_LOG_LEVEL='error')
        process = subprocess.Popen([str(binaries[language])], cwd=root, env=server_env, stdout=log, stderr=log)
        base = f'http://127.0.0.1:{port}'
        try:
            for attempt in range(200):
                if process.poll() is not None:
                    raise RuntimeError(f'{language} exited; inspect {log.name}')
                try:
                    with urllib.request.urlopen(base + '/health', timeout=1) as response:
                        assert json.load(response) == {'status': 'ok'}
                        return process, log, base
                except (OSError, urllib.error.URLError):
                    time.sleep(.05)
            raise RuntimeError('server startup timed out')
        except BaseException:
            process.terminate(); process.wait(); log.close(); raise
    def stop(process, log):
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill(); process.wait()
        log.close()
    process, log, _ = start('Ghi', template, 'seed')
    stop(process, log)
    expected_users = [{'id': str(uuid.UUID(int=i)), 'name': f'User {i}', 'email': f'user{i}@example.test'} for i in range(1, 1001)]
    with sqlite3.connect(template) as db:
        db.executemany('INSERT INTO users(id,name,email) VALUES (?,?,?)', [(u['id'],u['name'],u['email']) for u in expected_users])
    missing = str(uuid.UUID(int=99999))
    scenarios = {
        'page20': ('/api/v1/users/?limit=20&offset=0', 200, expected_users[:20]),
        'get': ('/api/v1/users/' + expected_users[0]['id'] + '/', 200, expected_users[0]),
        'missing': ('/api/v1/users/' + missing + '/', 404, {'error': {'code':2002,'type':'not_found','message':f'user {missing} was not found'}}),
    }
    references = {}
    records = []
    for sample in range(args.runs):
        for workers in (1, 16):
            for scenario, (path, status, expected) in scenarios.items():
                if scenario not in args.scenarios:
                    continue
                order = ('Ghi','Go') if sample % 2 == 0 else ('Go','Ghi')
                for language in order:
                    label = f'{sample}-{workers}-{scenario}-{language}'
                    db = work / (label + '.db')
                    shutil.copyfile(template, db)
                    process, log, base = start(language, db, label)
                    try:
                        try:
                            response = urllib.request.urlopen(base + path)
                        except urllib.error.HTTPError as error:
                            response = error
                        with response:
                            body = response.read()
                            assert response.status == status and json.loads(body) == expected
                            assert response.headers['Content-Type'] == 'application/json; charset=utf-8'
                            if scenario == 'page20':
                                assert response.headers['X-Limit'] == '20' and response.headers['X-Offset'] == '0'
                        if scenario in references:
                            assert references[scenario].read_bytes() == body, 'response bytes differ'
                        else:
                            references[scenario] = work / (scenario + '.json')
                            references[scenario].write_bytes(body)
                        command = [binaries['client'], '-url', base + path, '-body', references[scenario], '-status', status, '-workers', workers]
                        run(command + ['-requests', 300])
                        result = json.loads(run(command + ['-requests', args.requests]))
                        result.update(language=labels[language], scenario=scenario, sample=sample+1, peak_working_set_bytes=peak_memory(process.pid))
                        records.append(result)
                        print(json.dumps(result), flush=True)
                    finally:
                        stop(process, log)
    # Every measured route is read-only; preserve identical fixture contents.
    with sqlite3.connect(template) as db:
        assert db.execute('SELECT COUNT(*) FROM users').fetchone()[0] == 1000
    result = {
        'go': run([go,'version']).strip(), 'commit': run(['git','rev-parse','HEAD']).strip(),
        'gomaxprocs':8, 'users':1000, 'requests_per_sample':args.requests, 'runs':args.runs,
        'methodology':'Closed-loop localhost HTTP/1.1 keep-alive, independent server per sample, 300 warmup requests, identical SQLite snapshots, one DB connection, disabled info logs, JSON bytes checked on every response. Peak working set includes startup and warmup. Go baseline implements only the measured read routes. Ghi 404 captures exceptions; Go returns ordinary errors.',
        'comparison':'Current Ghi source versus a prebuilt Ghi baseline; source hashes below describe the current source only.' if args.baseline_ghi else 'Ghi versus Go',
        'binary_bytes':{labels[lang]:binaries[lang].stat().st_size for lang in ('Ghi','Go')},
        'binary_sha256':{labels[lang]:hashlib.sha256(binaries[lang].read_bytes()).hexdigest() for lang in ('Ghi','Go')},
        'timer':'Windows QueryPerformanceCounter; monotonic time.Since on other systems',
        'source_sha256':{str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in [*sorted((root/'benchmarks/http-api').rglob('*.go')), *sorted((root/'examples/ddd-api').rglob('*.ghi')), root/'examples/ddd-api/mojave.lock', root/'benchmarks/http-api/go/go.mod', root/'benchmarks/http-api/go/go.sum'] if p.is_file() and '.ghi/packages' not in p.as_posix()},
        'samples':records,
    }
    if args.baseline_ghi:
        result['methodology'] = result['methodology'].replace('Go baseline implements only the measured read routes. Ghi 404 captures exceptions; Go returns ordinary errors.', 'Both binaries are Ghi DDD servers. The baseline executable is supplied with --baseline-ghi; the current server is built from this checkout.')
    destination = root / args.output
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(json.dumps(result, indent=2)+'\n', encoding='utf-8')
    print('Saved', destination, flush=True)


if __name__ == '__main__':
    main()
