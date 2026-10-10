import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import release as r


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.dest = Path(self.temp.name)
        self.tag = "v1.2.3"

    def package(self, os_, arch, members=None):
        binary = "recipes.exe" if os_ == "windows" else "recipes"
        files = members or {binary: b"binary", "LICENSE": (r.ROOT / "LICENSE").read_bytes(), "README.md": (r.ROOT / "README.md").read_bytes()}
        path = self.dest / r.names(self.tag, os_, arch)
        if os_ == "windows":
            with zipfile.ZipFile(path, "w") as z:
                for name, data in files.items():
                    z.writestr(name, data)
        else:
            with tarfile.open(path, "w:gz") as t:
                for name, data in files.items():
                    member = tarfile.TarInfo(name)
                    member.size, member.mode = len(data), 0o755 if name == binary else 0o644
                    t.addfile(member, io.BytesIO(data))
        self.checksum(path)
        return path

    def checksum(self, path):
        Path(str(path) + ".sha256").write_text(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n")

    def test_tag_safety(self):
        for value in ("v0.0.0", "v1.2.3", "v123.456.789"):
            self.assertEqual(r.valid_tag(value), value)
        for value in ("1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc1", "v1.2.3+build", "v1.2.3\n", "v١.2.3", "v1.2.3;id", "--help", "../v1.2.3", "v1.2.3/extra"):
            with self.subTest(tag=value), self.assertRaises(ValueError):
                r.valid_tag(value)
        with self.assertRaises(ValueError):
            r.names(self.tag, "plan9", "amd64")

    @patch.object(r, "inspect_binary")
    def test_all_packages_and_collection(self, inspect):
        for os_, arch in r.TARGETS:
            self.package(os_, arch)
            self.assertEqual(r.verify_package(self.dest, self.tag, os_, arch), b"binary")
        checksums = r.verify_collection(self.dest, self.tag)
        self.assertEqual(len(checksums.splitlines()), 6)
        inspect.assert_called()
        (self.dest / "unexpected").touch()
        with self.assertRaises(ValueError):
            r.verify_collection(self.dest, self.tag)

    def test_checksum_corruption_and_missing_platform(self):
        path = self.package("linux", "amd64")
        path.write_bytes(path.read_bytes() + b"tamper")
        with self.assertRaises(ValueError):
            r.verify_package(self.dest, self.tag, "linux", "amd64")
        with self.assertRaises(ValueError):
            r.verify_collection(self.dest, self.tag)

    def test_archive_member_and_document_safety(self):
        for os_ in ("linux", "windows"):
            for files in ({"../recipes": b"bad", "LICENSE": b"bad", "README.md": b"bad"}, {"recipes.exe" if os_ == "windows" else "recipes": b"bad", "LICENSE": b"wrong", "README.md": b"wrong"}):
                self.package(os_, "amd64", files)
                with self.assertRaises(ValueError):
                    r.verify_package(self.dest, self.tag, os_, "amd64")

    @patch.object(r, "run")
    def test_build_metadata(self, run):
        settings = f'\tbuild\t-ldflags="-X={r.VERSION_SYMBOL}={self.tag}"\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=amd64\n\tbuild\tCGO_ENABLED=0\n'
        run.return_value = settings
        r.inspect_binary(Path("binary"), "linux", "amd64")
        for bad in (settings.replace("CGO_ENABLED=0", "CGO_ENABLED=1"), settings.replace("GOARCH=amd64", "GOARCH=arm64")):
            run.return_value = bad
            with self.assertRaises(ValueError):
                r.inspect_binary(Path("binary"), "linux", "amd64")

    @patch.object(r, "verify_package", return_value=b"binary")
    @patch.object(r, "run")
    def test_native_smoke_version_and_no_runtime_path(self, run, verify):
        run.side_effect = ["v1.2.3\n", "Usage: recipes", '{}']
        r.smoke(self.dest, self.tag, "linux", "amd64")
        for call in run.call_args_list:
            self.assertEqual(call.kwargs["env"]["PATH"], "")
        run.reset_mock()
        run.side_effect = ["0.1.0-go\n"]
        with self.assertRaises(ValueError):
            r.smoke(self.dest, self.tag, "linux", "amd64")
        self.assertEqual(run.call_count, 1)

    def environment(self):
        return {"GITHUB_REPOSITORY": "denwilliams/recipes-cli", "GITHUB_SHA": "a" * 40, "GITHUB_REF": "refs/tags/" + self.tag, "GITHUB_EVENT_NAME": "push"}

    @patch.object(r, "verify_collection")
    @patch.object(r, "run")
    def test_event_gating_before_any_api(self, run, verify):
        for key, value in (("GITHUB_EVENT_NAME", "pull_request"), ("GITHUB_EVENT_NAME", "workflow_dispatch"), ("GITHUB_REF", "refs/heads/main"), ("GITHUB_SHA", "bad"), ("GITHUB_REPOSITORY", "--help")):
            with patch.dict(os.environ, {**self.environment(), key: value}), self.assertRaises(ValueError):
                r.publish(self.dest, self.tag)
        run.assert_not_called()
        verify.assert_not_called()

    @patch.object(r, "remote_commit", return_value="a" * 40)
    @patch.object(r, "verify_collection", return_value="checksums\n")
    @patch.object(r, "run")
    @patch.object(r, "ensure_release_absent")
    def test_publish_fail_closed_and_success(self, absent, run, verify, remote):
        for os_, arch in r.TARGETS:
            (self.dest / r.names(self.tag, os_, arch)).write_bytes(b"asset")
        with patch.dict(os.environ, self.environment()):
            for error in (ValueError("release exists"), subprocess.CalledProcessError(1, "gh api")):
                absent.side_effect = error
                with self.assertRaises((ValueError, subprocess.CalledProcessError)):
                    r.publish(self.dest, self.tag)
                run.assert_not_called()
            absent.side_effect = None
            remote.return_value = "b" * 40
            with self.assertRaises(ValueError):
                r.publish(self.dest, self.tag)
            run.assert_not_called()
            remote.return_value = "a" * 40
            # Failed validation must stop before draft creation.
            verify.side_effect = ValueError("missing archive")
            with self.assertRaises(ValueError):
                r.publish(self.dest, self.tag)
            run.assert_not_called()
            verify.side_effect = None
            # Upload failure must never publish the draft.
            run.side_effect = subprocess.CalledProcessError(1, "gh release create")
            with self.assertRaises(subprocess.CalledProcessError):
                r.publish(self.dest, self.tag)
            self.assertEqual(run.call_count, 1)
            run.reset_mock()
            # Incomplete draft assets must never become public.
            run.side_effect = ["", json.dumps({"isDraft": True, "assets": []})]
            with self.assertRaises(ValueError):
                r.publish(self.dest, self.tag)
            self.assertEqual(run.call_count, 2)
            run.reset_mock()
            assets = [{"name": r.names(self.tag, os_, arch), "size": 5, "state": "uploaded"} for os_, arch in r.TARGETS] + [{"name": "SHA256SUMS", "size": len("checksums\n"), "state": "uploaded"}]
            draft = json.dumps({"isDraft": True, "assets": assets})
            remote.side_effect = ["a" * 40, "b" * 40]
            run.side_effect = ["", draft]
            with self.assertRaises(ValueError):
                r.publish(self.dest, self.tag)
            self.assertEqual(run.call_count, 2)
            remote.side_effect = None
            run.reset_mock()
            run.side_effect = ["", draft, ""]
            r.publish(self.dest, self.tag)
            commands = [call.args[0] for call in run.call_args_list]
            self.assertIn("--draft", commands[0])
            self.assertIn("--verify-tag", commands[0])
            self.assertEqual(commands[-1], ["gh", "release", "edit", self.tag, "--repo", "denwilliams/recipes-cli", "--draft=false"])
            self.assertFalse(any("--clobber" in command for command in commands))

    @patch.object(r, "run")
    def test_no_existing_published_release_or_draft(self, run):
        run.return_value = json.dumps([[{"tag_name": "v0.9.0", "draft": False}], []])
        r.ensure_release_absent("denwilliams/recipes-cli", self.tag)
        for draft in (True, False):
            run.return_value = json.dumps([[{"tag_name": "v0.9.0"}], [{"tag_name": self.tag, "draft": draft}]])
            with self.assertRaises(ValueError):
                r.ensure_release_absent("denwilliams/recipes-cli", self.tag)
        run.side_effect = subprocess.CalledProcessError(1, "gh api")
        with self.assertRaises(subprocess.CalledProcessError):
            r.ensure_release_absent("denwilliams/recipes-cli", self.tag)

    @patch.object(r, "run")
    def test_annotated_and_lightweight_tags(self, run):
        run.return_value = json.dumps({"object": {"type": "commit", "sha": "a" * 40}})
        self.assertEqual(r.remote_commit("denwilliams/recipes-cli", self.tag), "a" * 40)
        run.side_effect = [json.dumps({"object": {"type": "tag", "sha": "b" * 40}}), json.dumps({"object": {"type": "commit", "sha": "a" * 40}})]
        self.assertEqual(r.remote_commit("denwilliams/recipes-cli", self.tag), "a" * 40)


if __name__ == "__main__":
    unittest.main()
