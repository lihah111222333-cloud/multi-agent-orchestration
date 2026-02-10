"""Migration discovery and ordering utilities."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re

_MIGRATION_FILENAME_RE = re.compile(r"^(\d{4})_([a-z0-9_]+)\.sql$")


@dataclass(frozen=True)
class Migration:
    version: int
    name: str
    filename: str
    path: Path


def parse_migration_filename(filename: str) -> tuple[int, str]:
    """Parse migration filename into version and name.

    Expected format: NNNN_name.sql (name uses lowercase letters, numbers, underscores).
    """
    text = str(filename or "").strip()
    match = _MIGRATION_FILENAME_RE.fullmatch(text)
    if match is None:
        raise ValueError(f"invalid migration filename: {filename}")

    version = int(match.group(1))
    name = match.group(2)
    if not name:
        raise ValueError(f"invalid migration filename: {filename}")
    return version, name


def discover_migrations(directory: str | Path) -> list[Migration]:
    """Discover migrations from a directory and validate ordering rules."""
    folder = Path(directory)
    if not folder.exists() or not folder.is_dir():
        raise ValueError(f"migration directory not found: {folder}")

    migrations: list[Migration] = []
    seen_versions: dict[int, str] = {}

    for path in folder.iterdir():
        if not path.is_file() or path.suffix.lower() != ".sql":
            continue

        version, name = parse_migration_filename(path.name)
        if version in seen_versions:
            existing = seen_versions[version]
            raise ValueError(
                f"duplicate migration version {version:04d}: {existing} and {path.name}"
            )

        seen_versions[version] = path.name
        migrations.append(
            Migration(
                version=version,
                name=name,
                filename=path.name,
                path=path,
            )
        )

    migrations.sort(key=lambda item: item.version)

    for index, migration in enumerate(migrations):
        expected = index + 1
        if migration.version != expected:
            raise ValueError(
                f"non-contiguous migration versions: expected version {expected}, got {migration.version}"
            )

    return migrations
