"""Regression and mutation tests for the final translation review (no running service required)."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest

from check_translation_review import ROOT, scope_errors, validate_review


class TranslationReviewTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="artex-i18n-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        review = json.loads((ROOT / "localization/final-review-translations.json").read_text(encoding="utf-8"))
        paths = {"localization/final-review-translations.json"}
        paths.update(x["file"] for x in review["file_hashes"])
        paths.update(x["file"] for x in review["protected_files"])
        for name in paths:
            dest = self.root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, dest)
        self.review = review

    def change(self, name, old, new):
        path = self.root / name
        text = path.read_text(encoding="utf-8-sig")
        self.assertIn(old, text)
        path.write_text(text.replace(old, new), encoding="utf-8-sig", newline="\n")

    def test_current_review_passes(self):
        self.assertEqual(validate_review(self.root), [])

    def test_new_chinese_inside_fenced_instruction_is_detected(self):
        self.change("skills/api-recon/SKILL.md", "Phase 0 분류 + OUTDIR", "Phase 0 分类 + OUTDIR")
        self.assertTrue(any("unreviewed Chinese text" in e for e in validate_review(self.root)))

    def test_cross_example_task_reference_must_match(self):
        path = self.root / "skills/scopesentry/SKILL.md"
        after = path.read_text(encoding="utf-8-sig")
        self.assertIn('"name": "example-하위도메인수집"', after)
        after = after.replace('"name": "example-하위도메인수집"', '"name": "다른작업"')
        self.assertTrue(any("follow-up search" in e for e in scope_errors(self.review["scope_example_before"], after)))

    def test_example_api_keys_are_not_translatable(self):
        after = (self.root / "skills/scopesentry/SKILL.md").read_text(encoding="utf-8-sig")
        after = after.replace('"targetSource":', '"대상출처":')
        self.assertTrue(any("API keys" in e for e in scope_errors(self.review["scope_example_before"], after)))

    def test_config_non_comment_values_cannot_change(self):
        path = self.root / "config.example.json"
        config = json.loads(path.read_text(encoding="utf-8"))
        config["database"]["port"] += 1
        path.write_text(json.dumps(config, ensure_ascii=False), encoding="utf-8")
        self.assertIn("Example configuration functional values changed", validate_review(self.root))

    def test_cli_format_mismatch_is_detected(self):
        path = self.root / "localization/final-review-translations.json"
        review = json.loads(path.read_text(encoding="utf-8"))
        entry = next(x for x in review["entries"] if x["category"] == "cli_text" and "%d" in x["after"])
        entry["after"] = entry["after"].replace("%d", "%s")
        path.write_text(json.dumps(review, ensure_ascii=False), encoding="utf-8")
        self.assertTrue(any("CLI printf contract changed" in e for e in validate_review(self.root)))

    def test_bom_loss_is_detected(self):
        path = self.root / "skills/api-recon/SKILL.md"
        path.write_bytes(path.read_bytes().removeprefix(b"\xef\xbb\xbf"))
        self.assertTrue(any("missing UTF-8 BOM" in e for e in validate_review(self.root)))

    def test_legacy_protocol_file_must_remain_byte_identical(self):
        path = self.root / "web/src/lib/chat-mentions.ts"
        path.write_bytes(path.read_bytes() + b"\n")
        self.assertTrue(any("protected protocol/runtime file changed" in e for e in validate_review(self.root)))

    def test_real_chinese_error_recognition_is_preserved(self):
        self.change("skills/api-recon/reference.md", "未登录|请重新登录", "로그인안됨|다시로그인")
        problems = validate_review(self.root)
        self.assertTrue(any("required Chinese input-recognition" in e for e in problems))
        self.assertTrue(any("API Recon executable example changed" in e for e in problems))


if __name__ == "__main__":
    unittest.main()
