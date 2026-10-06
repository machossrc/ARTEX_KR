from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
for relative in ("selfupdate", "server/update.go", "update.sh", "web/src/app/(main)/_components/update-badge.tsx", "web/src/app/(main)/system/settings/_components/update-card.tsx"):
    assert not (root / relative).exists(), relative
patterns = re.compile(r"selfupdate[./]|RestartRequested|SetBootUpdateState|/api/update/(check|apply|rollback|stream)|autumn27/artex|api\.github\.com/repos/Autumn-27/ARTEX/releases", re.I)
files = list((root / "agent").glob("*.go")) + list((root / "server").glob("*.go")) + list((root / "cmd").rglob("*.go")) + list((root / "web/src").rglob("*.ts")) + list((root / "web/src").rglob("*.tsx"))
files += [root / name for name in ("start.sh", "start.bat", "install.sh", "docker-compose.yml", "Dockerfile", "Dockerfile.local", ".github/workflows/release.yml")]
for path in files:
    text = path.read_text(encoding="utf-8-sig")
    for index, line in enumerate(text.splitlines(), 1):
        assert not patterns.search(line), f"{path.relative_to(root)}:{index}: {line}"
print(f"PASS: upstream update entry points are absent in {len(files)} files")
