"""Build both implementations and compare alternating, checksum-verified runs."""

import argparse
import json
import os
from pathlib import Path
import platform
import statistics
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--iterations", type=int, default=50_000_000)
    parser.add_argument("--runs", type=int, default=7)
    args = parser.parse_args()
    if args.iterations < 1 or args.runs < 1:
        parser.error("iterations and runs must be positive")
    root = Path(__file__).resolve().parents[1]
    go = os.environ.get("GHI_GO", "go")
    version = subprocess.check_output([go, "version"], text=True).strip()
    records = []
    with tempfile.TemporaryDirectory(prefix="ghi-benchmark-") as directory:
        suffix = ".exe" if os.name == "nt" else ""
        binaries = {name: str(Path(directory) / (name + suffix)) for name in ("go", "ghi")}
        subprocess.run([go, "build", "-o", binaries["go"], "./benchmarks/go"], cwd=root, check=True)
        subprocess.run([go, "run", "./cmd/ghi", "build", "-o", binaries["ghi"], "./benchmarks/ghi"], cwd=root, check=True, stdout=subprocess.DEVNULL)
        for run in range(args.runs):
            for language in (("go", "ghi") if run % 2 == 0 else ("ghi", "go")):
                values = list(map(int, subprocess.check_output([binaries[language], str(args.iterations)], text=True).split()))
                records.append(dict(run=run, language=language, numeric_ns=values[0], object_ns=values[1], numeric_sum=values[2], object_sum=values[3]))
    expected = records[0]["numeric_sum"]
    if any(row["numeric_sum"] != expected or row["object_sum"] != expected for row in records):
        raise RuntimeError("benchmark results do not agree")
    medians = {language: {metric: statistics.median(row[metric] for row in records if row["language"] == language) for metric in ("numeric_ns", "object_ns")} for language in ("go", "ghi")}
    print(json.dumps(dict(platform=platform.platform(), toolchain=version, iterations=args.iterations, medians=medians, ratios={metric: medians["ghi"][metric] / medians["go"][metric] for metric in medians["go"]}, runs=records), indent=2))


if __name__ == "__main__":
    main()
