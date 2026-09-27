#!/usr/bin/env python3
"""Install this repository's self-contained personal Codex skill."""
import os
from pathlib import Path
import shutil

source = Path(__file__).resolve().parent / "fervid-ui-review"
home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))).expanduser()
target = home / "skills" / source.name
if target.is_symlink():
    raise SystemExit(f"Refusing to overwrite a symlink: {target}")
if target.exists() and not (target / "SKILL.md").is_file():
    raise SystemExit(f"Not a skill directory: {target}")
shutil.copytree(source, target, dirs_exist_ok=True, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
print(target)
