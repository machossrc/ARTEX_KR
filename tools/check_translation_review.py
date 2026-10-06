"""Check the final-review fixes without starting ARTEX, touching its DB, or calling an LLM.

Run: python -B tools/check_translation_review.py
This supplements the original 1509-entry catalog; it is not an exhaustive
claim that arbitrary user data or third-party tool responses are Korean.
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
HAN = re.compile(r"[\u3400-\u9fff]")
FENCES = re.compile(r"^```([^\n]*)\n(.*?)^```\s*$", re.M | re.S)
FORMAT = re.compile(r"%(?:\[[0-9]+\])?[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[vTtbcdoOqxXUeEfFgGspw%]")
PLACEHOLDERS = {
    "你的主机": "호스트", "你的密钥": "사용자키",
    "<项目ObjectID>": "<프로젝트ObjectID>", "<模板ObjectID>": "<템플릿ObjectID>",
    "<后续模块模板ObjectID>": "<후속모듈템플릿ObjectID>",
    "example-子域名收集": "example-하위도메인수집",
    "example-端口与漏洞": "example-포트와취약점", "某任务名": "작업이름",
}


def translate_example(value):
    if isinstance(value, str):
        for before, after in PLACEHOLDERS.items():
            value = value.replace(before, after)
        return value
    if isinstance(value, list):
        return [translate_example(v) for v in value]
    if isinstance(value, dict):
        # Keys are API protocol identifiers and must not be translated.
        return {k: translate_example(v) for k, v in value.items()}
    return value


def code_without_comments(text: str, language: str) -> str:
    if language == "bash":
        return "\n".join(line for line in text.splitlines() if not line.lstrip().startswith("#"))
    if language == "javascript":
        return re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    return text


def scope_errors(before: str, after: str) -> list[str]:
    problems = []
    old_blocks, new_blocks = FENCES.findall(before), FENCES.findall(after)
    if len(old_blocks) != len(new_blocks):
        return ["ScopeSentry fenced-block count changed"]
    parsed = []
    for index, ((old_lang, old), (new_lang, new)) in enumerate(zip(old_blocks, new_blocks), 1):
        if old_lang != new_lang:
            problems.append(f"ScopeSentry block {index}: language changed")
        if old_lang.strip() == "json":
            try:
                original, current = json.loads(old), json.loads(new)
                if translate_example(original) != current:
                    problems.append(f"ScopeSentry block {index}: API keys, types, values, or linked examples changed")
                parsed.append(current)
            except ValueError as error:
                problems.append(f"ScopeSentry block {index}: invalid JSON: {error}")
        elif old_lang.strip() == "mermaid":
            edges = r"^\s*([A-Z])(?:\[[^\]]*\])?\s*-->\s*([A-Z])"
            if re.findall(edges, old, re.M) != re.findall(edges, new, re.M):
                problems.append("ScopeSentry diagram edges changed")
    general = next((x for x in parsed if isinstance(x, dict) and x.get("targetSource") == "general"), None)
    followup = next((x for x in parsed if isinstance(x, dict) and x.get("targetSource") == "subdomain"), None)
    if not general or not followup or followup.get("search") != f'task=="{general["name"]}"':
        problems.append("ScopeSentry follow-up search no longer matches the first-stage task name")
    return problems


def validate_review(root: Path = ROOT) -> list[str]:
    review = json.loads((root / "localization/final-review-translations.json").read_text(encoding="utf-8"))
    errors = []
    sources = {}
    ids = set()
    for item in review["entries"]:
        if item["id"] in ids:
            errors.append(f"duplicate final-review ID {item['id']}")
        ids.add(item["id"])
        file = item["file"]
        if file not in sources:
            sources[file] = (root / file).read_text(encoding="utf-8-sig")
        if sources[file].count(item["after"]) < item["occurrences"]:
            errors.append(f"#{item['id']}: reviewed translation absent in {file}")
        if item["category"] == "cli_text" and FORMAT.findall(item["before"]) != FORMAT.findall(item["after"]):
            errors.append(f"#{item['id']}: CLI printf contract changed")
    for item in review["file_hashes"]:
        text = sources[item["file"]]
        if hashlib.sha256(text.encode("utf-8")).hexdigest() != item["after_sha256"]:
            errors.append(f"reviewed source hash changed: {item['file']}")
        raw = (root / item["file"]).read_bytes()
        if (root / item["file"]).suffix in (".go", ".ts", ".tsx", ".md") and not raw.startswith(b"\xef\xbb\xbf"):
            errors.append(f"missing UTF-8 BOM: {item['file']}")
        if "\x00" in text or "\ufeff" in text:
            errors.append(f"embedded NUL/BOM: {item['file']}")
    for item in review["protected_files"]:
        if hashlib.sha256((root / item["file"]).read_bytes()).hexdigest() != item["sha256"]:
            errors.append(f"protected protocol/runtime file changed: {item['file']}")
    allowed = {(x["file"], x["line"]) for x in review["allowed_han"]}
    for file in ("skills/api-recon/SKILL.md", "skills/api-recon/reference.md", "skills/scopesentry/SKILL.md", "config.example.json"):
        for number, line in enumerate(sources[file].splitlines(), 1):
            if HAN.search(line) and (file, line) not in allowed:
                errors.append(f"unreviewed Chinese text: {file}:{number}")
    for file, line in allowed:
        if line not in sources[file].splitlines():
            errors.append(f"required Chinese input-recognition pattern changed: {file}")
    errors.extend(scope_errors(review["scope_example_before"], sources["skills/scopesentry/SKILL.md"]))
    # Ignore only comments in example scripts. Actual commands, strings and
    # parameters (including Chinese login error recognition) must be identical.
    old_blocks = FENCES.findall(review["api_reference_before"])
    new_blocks = FENCES.findall(sources["skills/api-recon/reference.md"])
    if len(old_blocks) != len(new_blocks):
        errors.append("API Recon reference block count changed")
    else:
        for index, ((old_lang, old), (new_lang, new)) in enumerate(zip(old_blocks, new_blocks), 1):
            if old_lang != new_lang or code_without_comments(old, old_lang) != code_without_comments(new, new_lang):
                errors.append(f"API Recon executable example changed: block {index}")
    current = json.loads(sources["config.example.json"])
    strip_notes = lambda obj: {k: v for k, v in obj.items() if not k.startswith("_comment")}
    if strip_notes(current) != strip_notes(review["config_before"]):
        errors.append("Example configuration functional values changed")
    if (root / "config.example.json").read_bytes().startswith(b"\xef\xbb\xbf"):
        errors.append("Strict JSON configuration must remain BOM-free")
    return errors


if __name__ == "__main__":
    issues = validate_review()
    if issues:
        raise SystemExit("\n".join(issues))
    print("PASS: final-review translations, skill JSON/links, diagram edges, executable examples, UTF-8 and protected contracts")
