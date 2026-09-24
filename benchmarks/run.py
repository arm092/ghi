"""Equivalent workload measurements; see docs/benchmarks.md for interpretation."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import tempfile
import time


def capture(command, **kwargs):
    return subprocess.check_output(command, text=True, stderr=subprocess.PIPE, **kwargs).strip()


def checksum(size):
    cycles, tail = divmod(size, 256)
    return cycles * 32640 + tail * (tail - 1) // 2


def summary(values):
    return {"median": statistics.median(values), "min": min(values), "max": max(values)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--iterations", type=int, default=20_000_000)
    parser.add_argument("--objects", type=int, default=100_000)
    parser.add_argument("--runs", type=int, default=7)
    parser.add_argument("--build-runs", type=int, default=3)
    parser.add_argument("--ghi", help="Prebuilt compiler executable (default bin/ghi)")
    parser.add_argument("--output", help="JSON destination (default .work/benchmarks/latest.json)")
    args = parser.parse_args()
    if min(args.iterations, args.objects, args.runs, args.build_runs) < 1:
        parser.error("sizes and repetition counts must be positive")
    root = Path(__file__).resolve().parents[1]
    suffix = ".exe" if os.name == "nt" else ""
    ghi = str(Path(args.ghi).resolve()) if args.ghi else str(root / "bin" / ("ghi" + suffix))
    go = shutil.which(os.environ.get("GHI_GO", "go"))
    if not go or not Path(ghi).is_file():
        parser.error("Go and a prebuilt Ghi compiler are required; use GHI_GO and --ghi")
    env = dict(os.environ, GHI_GO=go, GOTOOLCHAIN="local", GOWORK="off", GO111MODULE="on", GOFLAGS="")
    metadata = {
        "utc": datetime.now(timezone.utc).isoformat(), "platform": platform.platform(),
        "machine": platform.machine(), "processor": platform.processor(), "logical_cpus": os.cpu_count(),
        "python": platform.python_version(), "go": capture([go, "version"], env=env),
        "ghi": capture([ghi, "version"], env=env),
        "ghi_sha256": hashlib.sha256(Path(ghi).read_bytes()).hexdigest(),
        "go_env": json.loads(capture([go, "env", "-json", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOMODCACHE"], env=env)),
        "runtime_env": {key: env.get(key) for key in ("GOMAXPROCS", "GOGC", "GOMEMLIMIT", "GODEBUG")},
        "sources_sha256": {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
                           for path in (root / "benchmarks/go/main.go", root / "benchmarks/ghi/main.ghi")},
    }
    builds, records, sizes = [], [], {}
    with tempfile.TemporaryDirectory(prefix="ghi-benchmark-") as directory:
        work = Path(directory)
        binaries = {name: str(work / (name + suffix)) for name in ("go", "ghi")}
        commands = {
            "go": [go, "build", "-o", binaries["go"], "./benchmarks/go"],
            "ghi": [ghi, "build", "-o", binaries["ghi"], "./benchmarks/ghi"],
        }
        # Each language gets an independent empty Go build cache for each cold run.
        # Warm builds immediately reuse that cache; OS file and module caches stay warm.
        for run in range(args.build_runs):
            for language in (("go", "ghi") if run % 2 == 0 else ("ghi", "go")):
                build_env = dict(env, GOCACHE=str(work / f"cache-{language}-{run}"))
                for state in ("cold", "warm"):
                    Path(binaries[language]).unlink(missing_ok=True)
                    start = time.perf_counter_ns()
                    capture(commands[language], cwd=root, env=build_env)
                    builds.append({"run": run, "language": language, "cache": state, "ns": time.perf_counter_ns() - start})
                sizes[language] = Path(binaries[language]).stat().st_size
        def execute(language):
            start = time.perf_counter_ns()
            result = json.loads(capture([binaries[language], str(args.iterations), str(args.objects)], env=env))
            result["process_ns"] = time.perf_counter_ns() - start
            for workload, size in (("numeric", args.iterations), ("dispatch", args.iterations), ("objects", args.objects)):
                if result[workload]["sum"] != checksum(size):
                    raise RuntimeError(f"{language} {workload} checksum mismatch: {result[workload]}")
            return result
        for language in binaries:
            execute(language)  # One unrecorded warm-up per binary.
        for run in range(args.runs):
            for language in (("go", "ghi") if run % 2 == 0 else ("ghi", "go")):
                records.append({"run": run, "language": language, **execute(language)})
    measurements = {}
    for language in ("go", "ghi"):
        rows = [row for row in records if row["language"] == language]
        measurements[language] = {
            "binary_bytes": sizes[language],
            "build_ns": {state: summary([row["ns"] for row in builds if row["language"] == language and row["cache"] == state]) for state in ("cold", "warm")},
            "workloads": {workload: {metric: summary([row[workload][metric] for row in rows]) for metric in rows[0][workload] if metric != "sum"} for workload in ("numeric", "dispatch", "objects")},
            "process_ns": summary([row["process_ns"] for row in rows]),
        }
    result = {"metadata": metadata, "parameters": vars(args), "measurements": measurements, "build_samples": builds, "runtime_samples": records}
    output = Path(args.output) if args.output else root / ".work/benchmarks/latest.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(output.resolve()), "measurements": measurements}, indent=2))


if __name__ == "__main__":
    main()
