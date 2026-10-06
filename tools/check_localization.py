"""Verify applied Go translations and generate the README's exact before/after appendix.

Run from any directory: python tools/check_localization.py --go /path/to/go --write
Use --check in CI. This only parses source and never starts ARTEX or calls an LLM.
"""
from __future__ import annotations

import argparse
import collections
import hashlib
import html
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BEGIN = "<!-- BEGIN GENERATED PROMPT TRANSLATIONS -->"
END = "<!-- END GENERATED PROMPT TRANSLATIONS -->"
FORMAT = re.compile(r'%(?:\[[0-9]+\])?[-+# 0]*(?:(?:\d+|\*)(?:\.(?:\d+|\*))?|\.(?:\d+|\*))?(?:\[[0-9]+\])?[vTtbcdoOqxXUeEfFgGspw%]')
TEMPLATE = re.compile(r'{{[\s\S]*?}}')


def fenced(text: str) -> str:
    fence = '`' * max(4, 1 + max((len(x) for x in re.findall(r'`+', text)), default=0))
    return f"{fence}text\n{text}\n{fence}\n"


def appendix(catalog: dict) -> str:
    entries = catalog['entries']
    primary = catalog['primary_prompts']
    translated = sum(x['status'] == 'translated' for x in entries)
    out = [BEGIN, "\n## 시스템 프롬프트 번역 전·후 — 실제 적용 본문\n",
           "아래 중국어 원문은 번역 전 소스에서 수집한 내용이고, 한국어 본문은 현재 Go 소스의 AST 문자열과 대조한 실제 적용 내용입니다. "
           "Go의 문자열 연결로 나뉜 보고서 프롬프트도 합쳐서 전후 전체 본문을 표시합니다. "
           "사용자의 목표·작업 디렉터리 같은 실행 시 변수는 여기에서 임의로 채우지 않습니다.\n",
           f"대조 목록은 **{len(entries):,}개**이며, **{translated:,}개 번역**과 **{len(entries)-translated}개 호환성 보존**으로 구분합니다. "
           f"이 절에는 핵심 프롬프트 **{len(primary)}개 전체 본문**과 추가 실행 지침의 전후를 싣습니다. "
           "도구 설명·오류·로그·SQL 표시 문구를 포함한 전체 대응표와 위치는 [기계 판독 가능한 번역 기록](localization/go-translations.json)에 있습니다.\n",
           "### 번역 범위와 의미 보존\n",
           "프롬프트의 자연어를 한국어로 옮기되 도구명, JSON 키, 상태값, Go 템플릿 변수, 서식 지정자, 수치 한도, "
           "권한 경계, 금지 조건, 증거 기준과 마무리 순서를 유지합니다. **원문이 중국어 응답을 명시한 경우 그 의미도 유지합니다**. "
           "즉 프롬프트 본문은 한국어이지만 ‘중국어로 답하세요’라는 요구 자체를 ‘한국어로 답하세요’로 바꾸지 않았습니다.\n",
           "판정기의 `decision` 값은 `allow` / `ask` / `deny` 그대로이며, `comment`의 "
           "`实际操作：…；成功后的后果：…；命中规则：…`는 기존 파서가 읽는 프로토콜 표식이므로 유지합니다. "
           "기존 중국어 인용·명령·오류 인식과 과거 DB 트리거를 찾는 비교 문자열도 보존하며, 한국어 지원은 별칭으로 추가합니다.\n",
           "사용자가 DB에 편집해 저장한 프롬프트와 과거 버전을 강제로 덮어쓰지 않습니다. "
           "기본값 변경의 실제 적용 여부는 현재 선택된 프롬프트 버전에 따라 달라집니다.\n",
           "**검증의 한계:** 소스·서식·프로토콜 검사와 로컬 회귀 테스트는 외부 LLM이 두 언어의 프롬프트에서 "
           "항상 같은 추론·문장·도구 선택을 생성한다는 증명이 아닙니다. 실제 공급자 비교 평가를 실행하지 않은 결과를 동등성 통과로 표시하지 않습니다.\n",
           "### 핵심 프롬프트 전체 비교\n"]
    for v in primary:
        label = html.escape(v['file'] + ' · ' + v['name'])
        out.extend([f"<details>\n<summary>{label}</summary>\n", "\n**번역 전 — 중국어 원문**\n", fenced(v['before']),
                    "\n**번역 후 — 소스에 적용된 한국어 본문**\n", fenced(v['after']),
                    f"\n원문 SHA-256: `{v['before_sha256']}`\n\n적용 본문 SHA-256: `{v['after_sha256']}`\n\n</details>\n"])
    out.append("\n### 추가 실행 지침·동적 프롬프트 조각의 전후 비교\n")
    # All selected fragments are fully printed. Other entries remain in the
    # linked machine-readable catalog to keep the README usable on GitHub.
    selected = set(range(39,45)) | set(range(46,50)) | set(range(52,62)) | set(range(64,66)) | set(range(68,86)) | set(range(87,92))
    selected |= set(range(381,398)) | {119,801,803,838,849} | set(range(871,879)) | set(range(903,907))
    selected |= set(range(1142,1150)) | set(range(1215,1228)) | {1421,1422,1426,1428,1432,1433,1434}
    for e in entries:
        if e['id'] not in selected:
            continue
        location = e['locations'][0]
        label = html.escape(f"#{e['id']} · {location['file']} · {', '.join(location['contexts'])}")
        out.extend([f"<details>\n<summary>{label}</summary>\n", f"\n{e['reason']}\n", "\n**번역 전**\n", fenced(e['before']),
                    "\n**번역 후 / 호환성 보존 결과**\n", fenced(e['after']), "\n</details>\n"])
    out.append("\n### 스킬 프롬프트 전체 번역 전·후\n")
    skill_path = ROOT / "localization/skill-translations.json"
    skill_audit = json.loads(skill_path.read_text(encoding="utf-8"))
    out.append("스킬은 모델이 읽는 실행 지침이므로 아래에 세 문서의 전체 원문과 적용 본문을 함께 기록합니다. 코드 블록과 인식용 예시는 보존하고 설명을 번역했습니다.\n")
    for v in skill_audit["files"]:
        out.extend([f"<details>\n<summary>{html.escape(v['file'])}</summary>\n",
                    "\n**번역 전 — 원문**\n", fenced(v["before"]),
                    "\n**번역 후 — 적용 본문**\n", fenced(v["after"])])
        if v.get("structural_note"):
            out.append("\n" + v["structural_note"] + "\n")
        out.append("\n</details>\n")
    out.append("\n도구 설명·메시지 외에도 [스킬 지침](localization/skill-translations.json), "
               "[설치·빌드 스크립트](localization/script-translations.json), "
               "[SQL 표시 문구](localization/schema-translations.json), "
               "[운영 문서](localization/document-translations.json), "
               "[변경 이력](localization/changelog-translations.json)의 전체 전후 기록을 보존합니다. "
               "개발자 주석, 외부 API 식별자 및 과거 검증 JSON의 실제 응답은 번역 대상으로 바꾸지 않았습니다.\n")
    out.extend(["\n### 재검증 방법\n",
                "`python tools/check_localization.py --check`는 현재 소스에서 Go AST를 새로 추출하여 "
                "번역 문자열·핵심 프롬프트·서식·템플릿·이 README 부록이 서로 일치하는지 확인합니다. "
                "Go가 PATH에 없으면 `--go <go 실행 파일 경로>`를 지정합니다. "
                "검토된 번역 기록을 변경한 뒤 부록을 갱신할 때만 `--write`를 사용합니다.\n", END])
    return '\n'.join(out)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', default=shutil.which('go'))
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--write', action='store_true')
    mode.add_argument('--check', action='store_true')
    args = parser.parse_args()
    if not args.go:
        parser.error('Go was not found; supply --go')
    go = str(Path(args.go).resolve())
    catalog = json.loads((ROOT / 'localization/go-translations.json').read_text(encoding='utf-8'))
    with tempfile.TemporaryDirectory(prefix='artex-ast-') as td:
        exported = Path(td) / 'source.json'
        subprocess.run([go, 'run', str(ROOT / 'tools/localization_ast.go'), str(ROOT), str(exported)], cwd=ROOT, check=True)
        source = json.loads(exported.read_text(encoding='utf-8'))
    counts = collections.Counter((v['file'], v['text']) for v in source['literals'])
    variables = {(v['file'], v['name']): v['text'] for v in source['variables']}
    problems = []
    ids = [e['id'] for e in catalog['entries']]
    if len(ids) != len(set(ids)):
        problems.append('duplicate translation IDs')
    for e in catalog['entries']:
        a, b = e['before'], e['after']
        if FORMAT.findall(a) != FORMAT.findall(b):
            problems.append(f"#{e['id']}: printf format changed")
        if TEMPLATE.findall(a) != TEMPLATE.findall(b):
            problems.append(f"#{e['id']}: template changed")
        if '\ufeff' in b:
            problems.append(f"#{e['id']}: BOM inside runtime text")
        for loc in e['locations']:
            if counts[(loc['file'], b)] < loc['minimum_occurrences']:
                problems.append(f"#{e['id']}: applied source mismatch in {loc['file']}")
    for v in catalog['primary_prompts']:
        if variables.get((v['file'], v['name'])) != v['after']:
            problems.append(f"primary prompt mismatch: {v['file']}:{v['name']}")
        for side in ('before', 'after'):
            if hashlib.sha256(v[side].encode()).hexdigest() != v[side+'_sha256']:
                problems.append(f"invalid {side} hash: {v['name']}")
    # Each document audit contains its exact final source, not an illustrative
    # translation. Keeping full before/after text also makes manual review easy.
    audits = ["skill", "script", "schema", "document", "changelog"]
    for name in audits:
        audit = json.loads((ROOT / f"localization/{name}-translations.json").read_text(encoding="utf-8"))
        for item in audit.get("files", [audit]):
            current = (ROOT / item["file"]).read_text(encoding="utf-8-sig")
            if current != item["after"]:
                problems.append(f"document source mismatch: {item['file']}")
            if "\x00" in current:
                problems.append(f"NUL in document: {item['file']}")
    for name in ["README.md", "skills/api-recon/SKILL.md", "skills/api-recon/reference.md", "skills/scopesentry/SKILL.md", "db/schema.sql"]:
        if not (ROOT / name).read_bytes().startswith(b"\xef\xbb\xbf"):
            problems.append(f"UTF-8 BOM missing: {name}")
    for name in ["build.sh", "dev.sh", "install.sh", "reset-password.sh"]:
        if not (ROOT / name).read_bytes().startswith(b"#!"):
            problems.append(f"shell shebang must be first bytes: {name}")
    if problems:
        raise SystemExit('\n'.join(problems))
    readme = ROOT / 'README.md'
    text = readme.read_text(encoding='utf-8-sig')
    generated = appendix(catalog)
    if BEGIN in text:
        if text.count(BEGIN) != 1 or text.count(END) != 1:
            raise SystemExit('README appendix markers are not unique')
        start, end = text.index(BEGIN), text.index(END)+len(END)
        updated = text[:start] + generated + text[end:]
    else:
        updated = text.rstrip() + '\n\n' + generated + '\n'
    if args.write:
        readme.write_text(updated, encoding='utf-8-sig', newline='\n')
    elif updated != text:
        raise SystemExit('README appendix differs from reviewed source: run --write after review')
    print(f"PASS: {len(ids)} applied strings; {len(catalog['primary_prompts'])} full prompts; printf/templates; README before/after")


if __name__ == '__main__':
    main()
