from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path


SCRIPTS_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS_DIR))

import check_repo  # noqa: E402


class CommitSubjectTests(unittest.TestCase):
    def test_accepts_conventional_subjects(self) -> None:
        valid = (
            "feat: add decision draft",
            "fix(authz): hide private link metadata",
            "docs(governance): define master ruleset",
            "refactor(api)!: version event envelope",
            "revert: restore deployment projection",
        )
        for subject in valid:
            with self.subTest(subject=subject):
                self.assertTrue(check_repo.is_valid_commit_subject(subject))

    def test_rejects_ambiguous_subjects(self) -> None:
        invalid = (
            "update files",
            "Feat: wrong case",
            "fix(authz) missing colon",
            "feature: unsupported type",
            "docs: ",
        )
        for subject in invalid:
            with self.subTest(subject=subject):
                self.assertFalse(check_repo.is_valid_commit_subject(subject))


class MarkdownTargetTests(unittest.TestCase):
    def test_resolves_relative_file_and_fragment(self) -> None:
        with tempfile.TemporaryDirectory() as temp_directory:
            root = Path(temp_directory)
            document = root / "docs" / "guide.md"
            target = root / "README.md"
            document.parent.mkdir()
            document.write_text("guide\n", encoding="utf-8")
            target.write_text("readme\n", encoding="utf-8")

            resolved = check_repo.markdown_local_target(
                document, "../README.md#start", root
            )

            self.assertEqual(target.resolve(), resolved)

    def test_ignores_external_and_anchor_links(self) -> None:
        root = Path("/tmp/example")
        document = root / "guide.md"
        self.assertIsNone(
            check_repo.markdown_local_target(document, "https://example.com/x", root)
        )
        self.assertIsNone(check_repo.markdown_local_target(document, "#section", root))

    def test_rejects_repository_escape(self) -> None:
        with tempfile.TemporaryDirectory() as temp_directory:
            root = Path(temp_directory)
            document = root / "guide.md"
            with self.assertRaisesRegex(ValueError, "逃逸仓库根目录"):
                check_repo.markdown_local_target(document, "../secret.txt", root)

    def test_rejects_root_relative_links(self) -> None:
        root = Path("/tmp/example")
        document = root / "guide.md"
        with self.assertRaisesRegex(ValueError, "相对链接"):
            check_repo.markdown_local_target(document, "/docs/guide.md", root)


class TextClassificationTests(unittest.TestCase):
    def test_classifies_known_text_files(self) -> None:
        self.assertTrue(check_repo.is_text_file(Path("AGENTS.md")))
        self.assertTrue(check_repo.is_text_file(Path("LICENSE")))
        self.assertTrue(check_repo.is_text_file(Path(".gitignore")))
        self.assertFalse(check_repo.is_text_file(Path("assets/logo.png")))


if __name__ == "__main__":
    unittest.main()
