#!/usr/bin/env python3
"""Offline release packaging/validation; only `publish` invokes GitHub APIs."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
TARGETS = [(os_, arch) for os_ in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
VERSION_SYMBOL = "github.com/denwilliams/recipes-cli/internal/cli.Version"


def valid_tag(tag):
    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", tag, flags=re.ASCII):
        raise ValueError("Expected a stable vX.Y.Z tag (no leading zeroes or suffixes)")
    return tag


def names(tag, os_, arch):
    valid_tag(tag)
    if (os_, arch) not in TARGETS:
        raise ValueError("Unsupported target")
    return f"recipes_{tag}_{os_}_{arch}." + ("zip" if os_ == "windows" else "tar.gz")


def run(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kwargs).stdout


def inspect_binary(path, os_, arch):
    info = run(["go", "version", "-m", str(path)])
    for setting in (f"GOOS={os_}", f"GOARCH={arch}", "CGO_ENABLED=0"):
        if not re.search(r"(?m)^\s*build\s+" + re.escape(setting) + r"$", info):
            raise ValueError(f"Missing build setting: {setting}")


def build(tag, os_, arch, dest):
    filename = names(tag, os_, arch)
    dest = Path(dest)
    dest.mkdir(parents=True, exist_ok=True)
    binary = "recipes.exe" if os_ == "windows" else "recipes"
    with tempfile.TemporaryDirectory() as temp:
        path = Path(temp) / binary
        env = {**os.environ, "GOOS": os_, "GOARCH": arch, "CGO_ENABLED": "0", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local"}
        run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", f"-X={VERSION_SYMBOL}={tag}", "-o", str(path), "./cmd/recipes"], cwd=ROOT, env=env)
        inspect_binary(path, os_, arch)
        files = {binary: path.read_bytes(), "LICENSE": (ROOT / "LICENSE").read_bytes(), "README.md": (ROOT / "README.md").read_bytes()}
        archive = dest / filename
        if archive.exists() or (dest / (filename + ".sha256")).exists():
            raise ValueError("Refusing to overwrite a package/checksum")
        if os_ == "windows":
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as z:
                for name, data in files.items():
                    z.writestr(name, data)
        else:
            with tarfile.open(archive, "w:gz") as t:
                for name, data in files.items():
                    member = tarfile.TarInfo(name)
                    member.size, member.mode = len(data), 0o755 if name == binary else 0o644
                    t.addfile(member, io.BytesIO(data))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (dest / (filename + ".sha256")).write_text(f"{digest}  {filename}\n", encoding="ascii")
        verify_package(dest, tag, os_, arch)


def verify_package(dest, tag, os_, arch):
    """Validate without extracting arbitrary paths, links, or duplicate members."""
    dest = Path(dest)
    filename = names(tag, os_, arch)
    archive = dest / filename
    if archive.is_symlink() or not archive.is_file():
        raise ValueError("Missing/non-regular archive")
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    checksum = dest / (filename + ".sha256")
    if checksum.is_symlink() or checksum.read_text(encoding="ascii") != f"{digest}  {filename}\n":
        raise ValueError("Invalid checksum")
    binary = "recipes.exe" if os_ == "windows" else "recipes"
    expected = {binary, "README.md", "LICENSE"}
    if os_ == "windows":
        with zipfile.ZipFile(archive) as z:
            members = z.infolist()
            if len(members) != 3 or {m.filename for m in members} != expected or any(m.is_dir() or m.file_size > 100_000_000 for m in members):
                raise ValueError("Invalid ZIP members")
            files = {m.filename: z.read(m) for m in members}
    else:
        with tarfile.open(archive) as t:
            members = t.getmembers()
            if len(members) != 3 or {m.name for m in members} != expected or any(not m.isfile() or m.size > 100_000_000 for m in members):
                raise ValueError("Invalid tar members")
            if next(m for m in members if m.name == binary).mode & 0o111 != 0o111:
                raise ValueError("Binary is not executable")
            files = {m.name: t.extractfile(m).read() for m in members}
    for doc in ("README.md", "LICENSE"):
        if files[doc] != (ROOT / doc).read_bytes():
            raise ValueError(f"Unexpected {doc}")
    with tempfile.TemporaryDirectory() as temp:
        path = Path(temp) / binary
        path.write_bytes(files[binary])
        inspect_binary(path, os_, arch)
    return files[binary]


def verify_collection(dest, tag):
    dest = Path(dest)
    expected = {n for os_, arch in TARGETS for n in (names(tag, os_, arch), names(tag, os_, arch) + ".sha256")}
    if {p.name for p in dest.iterdir()} != expected:
        raise ValueError("Expected exactly six archives and six per-archive checksums")
    for os_, arch in TARGETS:
        verify_package(dest, tag, os_, arch)
    lines = [(dest / (n + ".sha256")).read_text(encoding="ascii") for n in sorted(expected) if not n.endswith(".sha256")]
    return "".join(lines)


def smoke(dest, tag, os_, arch):
    data = verify_package(dest, tag, os_, arch)
    with tempfile.TemporaryDirectory() as temp:
        path = Path(temp) / ("recipes.exe" if os_ == "windows" else "recipes")
        path.write_bytes(data)
        path.chmod(0o755)
        env = {**os.environ, "PATH": "", "HOME": temp, "RECIPES_CONFIG": str(Path(temp) / "absent.json")}
        if run([str(path), "--version"], env=env) != tag + "\n":
            raise ValueError("Native CLI version mismatch")
        if "Usage:" not in run([str(path), "--help"], env=env):
            raise ValueError("Native CLI help failed")
        run([str(path), "providers", "status", "--json"], env=env)
        if (Path(temp) / "absent.json").exists():
            raise ValueError("Read-only smoke unexpectedly wrote config")


def remote_commit(repo, tag):
    obj = json.loads(run(["gh", "api", f"repos/{repo}/git/ref/tags/{tag}"]))["object"]
    for _ in range(8):
        if not re.fullmatch(r"[0-9a-f]{40}", obj["sha"]):
            raise ValueError("Invalid remote object SHA")
        if obj["type"] == "commit":
            return obj["sha"]
        if obj["type"] != "tag":
            break
        obj = json.loads(run(["gh", "api", f"repos/{repo}/git/tags/{obj['sha']}"]))["object"]
    raise ValueError("Tag does not resolve to a commit")


def ensure_release_absent(repo, tag):
    # The /releases/tags endpoint only finds published releases. Paginate the
    # authenticated release list to detect drafts too; API errors fail closed.
    pages = json.loads(run(["gh", "api", "--paginate", "--slurp", f"repos/{repo}/releases?per_page=100"]))
    if any(release["tag_name"] == tag for page in pages for release in page):
        raise ValueError("Release/draft already exists; refusing overwrite")


def publish(dest, tag):
    """Tag push only. Never replace any release, including a failed attempt's draft."""
    valid_tag(tag)
    repo, sha = os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_SHA"]
    if (os.environ.get("GITHUB_EVENT_NAME") != "push" or os.environ.get("GITHUB_REF") != f"refs/tags/{tag}"
            or not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo)
            or not re.fullmatch(r"[0-9a-f]{40}", sha)):
        raise ValueError("Publication requires a stable tag push")
    checksums = verify_collection(dest, tag)
    if remote_commit(repo, tag) != sha:
        raise ValueError("Remote tag moved from the tested commit")
    ensure_release_absent(repo, tag)
    dest = Path(dest)
    (dest / "SHA256SUMS").write_text(checksums, encoding="ascii")
    assets = [str(dest / names(tag, os_, arch)) for os_, arch in TARGETS] + [str(dest / "SHA256SUMS")]
    notes = f"Standalone Recipes CLI {tag}. Download the archive for your OS/architecture and verify it with SHA256SUMS. See the bundled README.md for installation and limitations.\n\nSource commit: {sha}"
    run(["gh", "release", "create", tag, *assets, "--repo", repo, "--verify-tag", "--draft", "--title", tag, "--notes", notes])
    release = json.loads(run(["gh", "release", "view", tag, "--repo", repo, "--json", "isDraft,assets"]))
    expected = {Path(asset).name: Path(asset).stat().st_size for asset in assets}
    if (not release["isDraft"] or len(release["assets"]) != len(expected)
            or {a["name"]: a["size"] for a in release["assets"]} != expected
            or any(a["state"] != "uploaded" for a in release["assets"])):
        raise ValueError("Uploaded draft is incomplete; not publishing")
    if remote_commit(repo, tag) != sha:
        raise ValueError("Remote tag moved during upload; not publishing")
    run(["gh", "release", "edit", tag, "--repo", repo, "--draft=false"])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("validate-tag", "build", "verify", "smoke", "publish"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--os", choices=("linux", "darwin", "windows"))
    parser.add_argument("--arch", choices=("amd64", "arm64"))
    parser.add_argument("--dir", default="dist")
    args = parser.parse_args()
    valid_tag(args.tag)
    if args.command in ("build", "smoke"):
        if not args.os or not args.arch:
            parser.error("--os and --arch required")
        if args.command == "build":
            build(args.tag, args.os, args.arch, args.dir)
        else:
            smoke(args.dir, args.tag, args.os, args.arch)
    elif args.command == "verify":
        print(verify_collection(args.dir, args.tag), end="")
    elif args.command == "publish":
        publish(args.dir, args.tag)


if __name__ == "__main__":
    main()
