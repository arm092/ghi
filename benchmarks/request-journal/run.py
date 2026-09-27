"""Compare measured Request Journal routes with an independent Go implementation."""
from pathlib import Path
import argparse
import concurrent.futures
from contextlib import closing
import hashlib
import importlib.util
import json
import os
import platform
import shutil
import socket
import sqlite3
import statistics
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('http_benchmark', ROOT / 'benchmarks/http-api/run.py')
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def fetch(url):
    try:
        response = OPENER.open(url, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, response.read(), response.headers


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--requests', type=int, default=10000)
    parser.add_argument('--runs', type=int, default=5)
    parser.add_argument('--output', default='tests/performance/results/request-journal.json')
    parser.add_argument('--baseline-ghi', type=Path, help='compare with a saved Ghi server instead of Go')
    parser.add_argument('--scenarios', nargs='+', choices=('page20','get','history','missing','invalid'), default=('page20','get','history','missing','invalid'))
    parser.add_argument('--profile', action='store_true', help='capture separate CPU profiles after timing')
    args = parser.parse_args()
    if args.requests < 100 or args.runs < 1:
        parser.error('at least 100 requests and one run required')
    (ROOT / '.work').mkdir(exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix='journal-bench-', dir=ROOT / '.work'))
    go = shutil.which('go')
    if go is None:
        parser.error('Go must be on PATH')
    env = dict(os.environ, GHI_GO=go, GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='', GOMAXPROCS='8', CGO_ENABLED='0')
    suffix = '.exe' if os.name == 'nt' else ''
    binaries = {key: work / (key + suffix) for key in ('compiler', 'Ghi', 'Go', 'client')}
    def run(command, cwd=ROOT):
        try:
            return subprocess.check_output([str(v) for v in command], cwd=cwd, env=env, text=True, stderr=subprocess.STDOUT)
        except subprocess.CalledProcessError as error:
            print(error.output, flush=True)
            raise
    project = work / 'project'
    shutil.copytree(ROOT / 'services/request-journal', project, ignore=shutil.ignore_patterns('.ghi', '.idea', 'bin', '__pycache__', '*.db*'))
    instrument = ROOT / 'benchmarks/request-journal/metrics.ghi'
    shutil.copyfile(instrument, project / 'benchmark_metrics.ghi')
    mainfile = project / 'main.ghi'
    mainfile.write_text(mainfile.read_text(encoding='utf-8').replace('func main() {', 'func main() {\n startBenchmarkMetrics()'), encoding='utf-8')
    locked = {item['path']: item['version'] for item in json.loads((project / 'mojave.lock').read_text())['go']['modules']}
    for line in run([go, 'list', '-m', '-f', '{{.Path}} {{.Version}}', 'all'], ROOT / 'benchmarks/request-journal/go').splitlines():
        fields = line.split()
        if len(fields) == 2:
            assert locked.get(fields[0]) == fields[1], 'dependency mismatch: ' + line
    run([go, 'build', '-o', binaries['compiler'], './cmd/ghi'])
    mojave = work / ('mojave' + suffix)
    run([go, 'build', '-o', mojave, 'github.com/arm092/mojave/cmd/mojave'])
    run([mojave, 'install'], project)
    run([binaries['compiler'], 'build', '-o', binaries['Ghi'], project])
    if args.baseline_ghi:
        shutil.copy2(args.baseline_ghi.resolve(), binaries['Go'])
    else:
        run([go, 'build', '-o', binaries['Go'], '.'], ROOT / 'benchmarks/request-journal/go')
    labels = {'Ghi':'Ghi-after','Go':'Ghi-before'} if args.baseline_ghi else {'Ghi':'Ghi','Go':'Go'}
    run([go, 'build', '-o', binaries['client'], './benchmarks/http-api/client'])
    print('Built both servers and client:', work, flush=True)
    sources=[*sorted((ROOT/'services/request-journal').rglob('*.ghi')), *sorted((ROOT/'benchmarks/request-journal').rglob('*.go')), *sorted((ROOT/'benchmarks/http-api/client').glob('*.go')), instrument, Path(__file__).resolve(), ROOT/'services/request-journal/mojave.lock', ROOT/'benchmarks/request-journal/go/go.mod', ROOT/'benchmarks/request-journal/go/go.sum']
    source_hashes={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sources if '.ghi' not in p.relative_to(ROOT).parts}
    compiler_source_hash=hashlib.sha256(b''.join(str(p.relative_to(ROOT)).encode()+b'\0'+p.read_bytes() for folder in ('cmd/ghi','internal') for p in sorted((ROOT/folder).rglob('*.go')))).hexdigest()


    def free_port():
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            return sock.getsockname()[1]
    def start(language, database, label):
        port, metrics_port = free_port(), free_port()
        while metrics_port == port:
            metrics_port = free_port()
        log = (work / (label + '.log')).open('w')
        server_env = dict(env, JOURNAL_ADDR=f'127.0.0.1:{port}', JOURNAL_DB=str(database), JOURNAL_MIGRATIONS=str(project / 'migrations'), JOURNAL_METRICS_ADDR=f'127.0.0.1:{metrics_port}')
        process = subprocess.Popen([str(binaries[language])], cwd=project, env=server_env, stdout=log, stderr=log)
        base, metrics = f'http://127.0.0.1:{port}', f'http://127.0.0.1:{metrics_port}'
        try:
            for _ in range(200):
                if process.poll() is not None:
                    raise RuntimeError(Path(log.name).read_text())
                try:
                    assert json.loads(fetch(base + '/health')[1]) == {'status': 'ok'}
                    assert fetch(metrics + '/bench/memory')[0] == 200
                    return process, log, base, metrics
                except OSError:
                    time.sleep(.05)
            raise RuntimeError('startup timed out')
        except BaseException:
            stop(process, log)
            raise
    def stop(process, log):
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill(); process.wait()
        log.close()
    template = work / 'template.db'
    process, log, _, _ = start('Ghi', template, 'migrate')
    stop(process, log)
    stamp = '2026-09-28T00:00:00Z'
    with closing(sqlite3.connect(template)) as db:
        db.executemany('INSERT INTO requests(id,title,status,created_at,updated_at) VALUES (?,?,?,?,?)', [(i, f'Request {i}', 'open', stamp, stamp) for i in range(1,1001)])
        db.executemany('INSERT INTO request_events(request_id,status,created_at) VALUES (?,?,?)', [(i,'open',stamp) for i in range(1,1001)])
        db.commit()
        db.execute('PRAGMA wal_checkpoint(TRUNCATE)')
    item = lambda i: {'id':i, 'title':f'Request {i}', 'status':'open', 'created_at':stamp, 'updated_at':stamp}
    scenarios = {
        'page20': ('/requests?limit=20&offset=0', 200, [item(i) for i in range(1000,980,-1)]),
        'get': ('/requests/1', 200, item(1)),
        'history': ('/requests/1/history', 200, [{'id':1,'status':'open','created_at':stamp}]),
        'missing': ('/requests/999999', 404, {'error':'request not found'}),
        'invalid': ('/requests/not-an-id', 422, {'error':'id must be a positive integer','fields':[{'field':'id','code':'positive_integer'}]}),
    }
    scenarios = {key:value for key,value in scenarios.items() if key in args.scenarios}
    if args.profile and 'missing' not in scenarios:
        parser.error('--profile requires the missing scenario')
    references, records = {}, []
    for sample in range(args.runs):
        for workers in (1, 16):
            for scenario, (path, status, expected) in scenarios.items():
                for language in (('Ghi','Go') if sample % 2 == 0 else ('Go','Ghi')):
                    label = f'{sample}-{workers}-{scenario}-{language}'
                    database = work / (label + '.db')
                    shutil.copyfile(template, database)
                    process, log, base, metrics = start(language, database, label)
                    try:
                        code, body, headers = fetch(base + path)
                        assert code == status and json.loads(body) == expected, (language, scenario, code, body)
                        assert headers['Content-Type'] == 'application/json'
                        if scenario not in references:
                            references[scenario] = work / (scenario + '.json')
                            references[scenario].write_bytes(body)
                        assert references[scenario].read_bytes() == body, 'different response bytes'
                        command = [binaries['client'], '-url', base + path, '-body', references[scenario], '-status', status, '-workers', workers]
                        run(command + ['-requests', 300])
                        before = json.loads(fetch(metrics + '/bench/memory')[1])
                        result = json.loads(run(command + ['-requests', args.requests]))
                        after = json.loads(fetch(metrics + '/bench/memory')[1])
                        result.update(language=labels[language], scenario=scenario, sample=sample+1,
                                      bytes_per_request=(after['bytes']-before['bytes'])/args.requests,
                                      allocs_per_request=(after['allocations']-before['allocations'])/args.requests,
                                      heap_bytes=after['heap'], peak_working_set_bytes=helpers.peak_memory(process.pid))
                        records.append(result)
                        print(json.dumps(result), flush=True)
                    finally:
                        stop(process, log)
    if args.profile:
        for language in ('Ghi','Go'):
            database=work / (language+'-profile.db');shutil.copyfile(template,database)
            process,log,base,metrics=start(language,database,language+'-profile')
            try:
                with concurrent.futures.ThreadPoolExecutor() as pool:
                    future=pool.submit(fetch,metrics+'/debug/pprof/profile?seconds=5')
                    run([binaries['client'],'-url',base+'/requests/999999','-body',references['missing'],'-status',404,'-workers',16,'-requests',100000])
                    code,body,_=future.result();assert code==200
                    profile_path=work/(language+'.cpu');profile_path.write_bytes(body)
                    (work/(language+'-cpu-top.txt')).write_text(run([go,'tool','pprof','-top',binaries[language],profile_path]))
                code,body,_=fetch(metrics+'/debug/pprof/allocs');assert code==200
                profile_path=work/(language+'.allocs');profile_path.write_bytes(body)
                (work/(language+'-allocs-top.txt')).write_text(run([go,'tool','pprof','-top','-sample_index=alloc_space',binaries[language],profile_path]))
            finally:
                stop(process,log)
    output={'go':run([go,'version']).strip(),'commit':run(['git','rev-parse','HEAD']).strip(),'platform':platform.platform(),'processor':platform.processor(),'gomaxprocs':8,'runs':args.runs,'requests_per_sample':args.requests,'work':str(work),
            'compiler_source_sha256':compiler_source_hash, 'compiler_binary_sha256':hashlib.sha256(binaries['compiler'].read_bytes()).hexdigest(),
            'methodology':'Closed-loop localhost HTTP/1.1 keep-alive, 300 warmup requests, alternating implementation order, new server and identical 1000-row SQLite snapshot for every sample. Same Chi/SQLite dependencies, WAL, FK/busy timeout, one DB connection, context/server timeouts, SQL and JSON. Reference Go implements measured read routes only; startup/migrations/writes are not compared. Ghi uses typed exceptions and the published validation package; Go uses ordinary errors and equivalent direct validation. Differences include these implementation choices, not only compiler overhead. MemStats deltas include server HTTP work and the amortized stats request; peak working set includes startup/warmup. No race/coverage/debug instrumentation. Profiles are a separate missing-record workload after timings.',
            'comparison':'Ghi before/after' if args.baseline_ghi else 'Ghi versus Go',
            'binary_bytes':{labels[k]:binaries[k].stat().st_size for k in ('Ghi','Go')},'binary_sha256':{labels[k]:hashlib.sha256(binaries[k].read_bytes()).hexdigest() for k in ('Ghi','Go')},
            'source_sha256':source_hashes,'samples':records}
    if args.baseline_ghi:
        output['methodology'] = output['methodology'].replace('Reference Go implements measured read routes only; startup/migrations/writes are not compared. Ghi uses typed exceptions and the published validation package; Go uses ordinary errors and equivalent direct validation. Differences include these implementation choices, not only compiler overhead.', 'Both binaries are Ghi; baseline supplied with --baseline-ghi. Source hashes describe current implementation only. The before binary SHA-256 links to the prior full comparison.')
    destination=ROOT/args.output;destination.parent.mkdir(parents=True,exist_ok=True);destination.write_text(json.dumps(output,indent=2)+'\n',encoding='utf-8')
    for scenario in scenarios:
        for workers in (1,16):
            rows={labels[lang]:[r for r in records if r['language']==labels[lang] and r['scenario']==scenario and r['workers']==workers] for lang in ('Ghi','Go')}
            print(scenario,workers,{lang:{key:round(statistics.median(r[key] for r in values),3) for key in ('requests_per_second','p95_ms','p99_ms','bytes_per_request','allocs_per_request')} for lang,values in rows.items()})
    print('Saved',destination,flush=True)


if __name__=='__main__':
    main()
