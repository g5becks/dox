#!/usr/bin/env python3
"""Validate snapshot checksums/platforms and record every built binary's size."""
import hashlib
import json
import sys
from pathlib import Path


def summarize(dist):
    artifacts = json.loads((dist / "artifacts.json").read_text())
    for line in (dist / "checksums.txt").read_text().splitlines():
        expected, name = line.split(None, 1)
        name = name.strip().lstrip("*")
        if Path(name).name != name:
            raise ValueError(f"unexpected checksum filename: {name}")
        if hashlib.sha256((dist / name).read_bytes()).hexdigest() != expected:
            raise ValueError(f"checksum mismatch: {name}")
    platforms = {
        platform
        for artifact in artifacts
        if artifact["type"] == "Docker Image"
        for platform in artifact.get("extra", {}).get("Platforms", [])
    }
    if platforms != {"linux/amd64", "linux/arm64"}:
        raise ValueError(f"missing snapshot image platforms: {platforms}")
    patterns = {
        "darwin/amd64": "dox_darwin_amd64*/dox",
        "darwin/arm64": "dox_darwin_arm64*/dox",
        "darwin/universal": "dox_darwin_all/dox",
        "linux/amd64": "dox_linux_amd64*/dox",
        "linux/arm64": "dox_linux_arm64*/dox",
        "linux/arm/v7": "dox_linux_arm_7/dox",
        "freebsd/amd64": "dox_freebsd_amd64*/dox",
        "windows/amd64": "dox_windows_amd64*/dox.exe",
    }
    sizes = []
    for platform, pattern in patterns.items():
        files = list(dist.glob(pattern))
        if len(files) != 1 or not files[0].is_file():
            raise ValueError(f"expected one {platform} binary, got {files}")
        binary = files[0].resolve()
        size = binary.stat().st_size
        if size == 0:
            raise ValueError(f"empty binary: {binary}")
        sizes.append({"platform": platform, "path": str(binary), "bytes": size})
        print(f"{platform}\t{binary}\t{size}")
    (dist.parent / "binary-sizes.json").write_text(json.dumps(sizes, indent=2) + "\n")


if __name__ == "__main__":
    summarize(Path(sys.argv[1]).resolve())
