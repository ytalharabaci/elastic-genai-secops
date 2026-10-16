"""Run sample events through the filter section of a Logstash pipeline and check the result.

Each tests/pipelines/<name>.yml names a pipeline file and lists cases:

    pipeline: integrations/sources/kaspersky/logstash.conf
    cases:
      - name: virus found (CEF)
        event: {message: "CEF:0|KasperskyLab|..."}      # the event as the input would deliver it
        expect: {host.name: win10-01, event.kind: alert}   # dotted field -> value
        absent: [tags]                                     # fields that must not exist (optional)

The pipeline's input and output are replaced with stdin/stdout; the filter runs unchanged in the
Logstash image of the stack version. Needs Docker.

    python tests/pipelines/run.py                 # every file
    python tests/pipelines/run.py kaspersky       # one file
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
IMAGE = f"docker.elastic.co/logstash/logstash:{os.getenv('ELASTIC_VERSION', '8.13.4')}"


def filter_section(pipeline: Path) -> str:
    """The text from the top-level 'filter {' line to the line before the top-level 'output {'."""
    lines = pipeline.read_text(encoding="utf-8").splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith("filter {"))
    end = next(i for i, line in enumerate(lines) if line.startswith("output {"))
    return "\n".join(lines[start:end])


def get(doc: dict, dotted: str) -> object:
    for part in dotted.split("."):
        if not isinstance(doc, dict) or part not in doc:
            return KeyError
        doc = doc[part]
    return doc


def run(spec_file: Path) -> list[str]:
    spec = yaml.safe_load(spec_file.read_text(encoding="utf-8"))
    config = ("input { stdin { codec => json_lines } }\n" + filter_section(ROOT / spec["pipeline"])
              + "\noutput { stdout { codec => json_lines } }\n")
    events = "".join(json.dumps({**case["event"], "test_case": i}) + "\n"
                     for i, case in enumerate(spec["cases"]))
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / "test.conf").write_text(config, encoding="utf-8")
        proc = subprocess.run(
            ["docker", "run", "--rm", "-i", "-v", f"{tmp}:/cfg:ro", IMAGE,
             "-f", "/cfg/test.conf", "-w", "1", "--log.level", "error"],
            input=events, capture_output=True, text=True, timeout=600,
            env={**os.environ, "MSYS_NO_PATHCONV": "1"})
    outputs = {}
    for line in proc.stdout.splitlines():
        if line.startswith("{"):
            doc = json.loads(line)
            outputs[doc.get("test_case")] = doc
    failures = []
    for i, case in enumerate(spec["cases"]):
        doc = outputs.get(i)
        if doc is None:
            failures.append(f"{spec_file.stem}: {case['name']}: no output\n{proc.stderr[-2000:]}")
            continue
        for field, want in (case.get("expect") or {}).items():
            got = get(doc, field)
            if got != want:
                failures.append(f"{spec_file.stem}: {case['name']}: {field} = "
                                f"{'<missing>' if got is KeyError else repr(got)}, want {want!r}")
        for field in case.get("absent") or []:
            if get(doc, field) is not KeyError:
                failures.append(f"{spec_file.stem}: {case['name']}: {field} should be absent, "
                                f"got {get(doc, field)!r}")
        print(f"  {'FAIL' if any(case['name'] in f for f in failures) else 'PASS'}  "
              f"{spec_file.stem}: {case['name']}")
    return failures


def main() -> None:
    names = sys.argv[1:]
    files = sorted(p for p in Path(__file__).parent.glob("*.yml") if not names or p.stem in names)
    failures = [f for spec in files for f in run(spec)]
    for f in failures:
        print(f, file=sys.stderr)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
