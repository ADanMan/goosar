#!/usr/bin/env python3
"""T-025: verify that no table or column name in server2/migrations/*.up.sql
collides with server/migrations/001_init.up.sql, except the allowed common
names (id, created_at, updated_at, workspace_id).

Usage: python3 server2/migrations/check_names.py
Exits 0 and prints "0 пересечений" when there are no collisions, exits 1 and
lists every collision otherwise.
"""
import glob
import os
import re
import sys

ALLOWED = {"id", "created_at", "updated_at", "workspace_id"}

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
OLD_SCHEMA = os.path.join(REPO_ROOT, "server", "migrations", "001_init.up.sql")
NEW_SCHEMA_GLOB = os.path.join(REPO_ROOT, "server2", "migrations", "*.up.sql")

CREATE_TABLE_RE = re.compile(
    r"CREATE TABLE\s+(?:public\.)?(\w+)\s*\((.*?)\n\);", re.S | re.I
)
ALTER_ADD_COLUMN_RE = re.compile(
    r"ALTER TABLE\s+(?:public\.)?(\w+)\s+ADD COLUMN\s+(?:IF NOT EXISTS\s+)?(\w+)",
    re.I,
)
COLUMN_LINE_RE = re.compile(r"^(\w+)\s+")
NON_COLUMN_KEYWORDS = {
    "CONSTRAINT",
    "PRIMARY",
    "UNIQUE",
    "CHECK",
    "FOREIGN",
    "EXCLUDE",
    "LIKE",
}


def extract_names(sql_text: str):
    """Return (table_names, column_names) found in a schema's CREATE TABLE bodies."""
    tables = set()
    columns = set()
    for name, body in CREATE_TABLE_RE.findall(sql_text):
        tables.add(name)
        depth = 0
        current = []
        lines = []
        # split the body on top-level commas (types like numeric(20,6) contain commas)
        for ch in body:
            if ch == "(":
                depth += 1
            elif ch == ")":
                depth -= 1
            if ch == "," and depth == 0:
                lines.append("".join(current))
                current = []
            else:
                current.append(ch)
        if current:
            lines.append("".join(current))
        for line in lines:
            line = line.strip()
            if not line:
                continue
            m = COLUMN_LINE_RE.match(line)
            if m and m.group(1).upper() not in NON_COLUMN_KEYWORDS:
                columns.add(m.group(1))
    for tbl, col in ALTER_ADD_COLUMN_RE.findall(sql_text):
        columns.add(col)
    return tables, columns


def load(path: str) -> str:
    with open(path, "r", encoding="utf-8") as fh:
        return fh.read()


def main() -> int:
    if not os.path.isfile(OLD_SCHEMA):
        print(f"не найден старый файл миграции: {OLD_SCHEMA}", file=sys.stderr)
        return 2

    old_tables, old_columns = extract_names(load(OLD_SCHEMA))

    new_tables, new_columns = set(), set()
    new_files = sorted(glob.glob(NEW_SCHEMA_GLOB))
    if not new_files:
        print(f"не найдено ни одного файла по маске: {NEW_SCHEMA_GLOB}", file=sys.stderr)
        return 2
    for path in new_files:
        t, c = extract_names(load(path))
        new_tables |= t
        new_columns |= c

    table_collisions = sorted(new_tables & old_tables)
    column_collisions = sorted((new_columns & old_columns) - ALLOWED)

    print(f"старая схема: {len(old_tables)} таблиц, {len(old_columns)} колонок")
    print(f"новая схема (server2): {len(new_tables)} таблиц, {len(new_columns)} колонок")
    print(f"файлов миграций проверено: {len(new_files)}")
    print()

    if table_collisions:
        print(f"СОВПАДЕНИЯ ИМЁН ТАБЛИЦ ({len(table_collisions)}):")
        for t in table_collisions:
            print(f"  - {t}")
    else:
        print("совпадений имён таблиц: 0")

    if column_collisions:
        print(f"СОВПАДЕНИЯ ИМЁН КОЛОНОК ({len(column_collisions)}, вне списка исключений {sorted(ALLOWED)}):")
        for c in column_collisions:
            print(f"  - {c}")
    else:
        print("совпадений имён колонок: 0")

    total = len(table_collisions) + len(column_collisions)
    print()
    print(f"итого пересечений: {total}")
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
